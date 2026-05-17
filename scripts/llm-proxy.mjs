#!/usr/bin/env node
/**
 * Local LLM Proxy — Ollama ↔ LiteLLM bridge
 *
 * Listens on localhost:11434 (Ollama default) and translates Ollama API calls
 * to OpenAI-compatible calls forwarded to litellm:4000.
 *
 * Handles:
 *   GET  /api/tags              → GET  /v1/models  (reformatted)
 *   GET  /api/version           → synthetic response
 *   POST /api/chat              → POST /v1/chat/completions  (streaming + non-streaming)
 *   POST /api/generate          → POST /v1/chat/completions  (prompt → user message)
 *   everything else             → forwarded as-is
 *
 * Usage: node llm-proxy.mjs
 */

import http from "http";
import fs from "fs";
import path from "path";

const LISTEN_PORT = 11434;
const LISTEN_HOST = "127.0.0.1";
const TARGET_HOST = "litellm";
const TARGET_PORT = 4000;
const AUTH_KEY = "sk-UmD4fSIUZzTJaqITjDsBFg";

// ---------------------------------------------------------------------------
// Logging — writes to both stdout and a rotating log file
// ---------------------------------------------------------------------------

const LOG_FILE = path.join(
  path.dirname(new URL(import.meta.url).pathname),
  "llm-proxy.log"
);
const logStream = fs.createWriteStream(LOG_FILE, { flags: "a" });

function ts() {
  return new Date().toISOString();
}

function log(...args) {
  const line = `${ts()} ${args.join(" ")}`;
  console.log(line);
  logStream.write(line + "\n");
}

function logErr(...args) {
  const line = `${ts()} ERROR ${args.join(" ")}`;
  console.error(line);
  logStream.write(line + "\n");
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks)));
    req.on("error", reject);
  });
}

function litellmRequest(method, path, body, extraHeaders = {}) {
  return new Promise((resolve, reject) => {
    const bodyBuf = body ? Buffer.from(JSON.stringify(body)) : null;
    const headers = {
      "content-type": "application/json",
      authorization: `Bearer ${AUTH_KEY}`,
      host: `${TARGET_HOST}:${TARGET_PORT}`,
      ...extraHeaders,
    };
    if (bodyBuf) headers["content-length"] = bodyBuf.length;

    const req = http.request(
      { hostname: TARGET_HOST, port: TARGET_PORT, path, method, headers },
      resolve
    );
    req.on("error", reject);
    if (bodyBuf) req.write(bodyBuf);
    req.end();
  });
}

async function readJson(res) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    res.on("data", (c) => chunks.push(c));
    res.on("end", () => {
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString()));
      } catch (e) {
        reject(e);
      }
    });
    res.on("error", reject);
  });
}

function sendJson(res, status, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(status, {
    "content-type": "application/json",
    "content-length": Buffer.byteLength(body),
  });
  res.end(body);
}

// ---------------------------------------------------------------------------
// Route handlers
// ---------------------------------------------------------------------------

// GET /api/tags  →  GET /v1/models  →  Ollama format
async function handleTags(res) {
  try {
    const upstream = await litellmRequest("GET", "/v1/models");
    const data = await readJson(upstream);
    const models = (data.data || []).map((m) => ({
      name: m.id,
      model: m.id,
      modified_at: new Date().toISOString(),
      size: 0,
      digest: "",
      details: { format: "gguf", family: "unknown", parameter_size: "unknown", quantization_level: "unknown" },
    }));
    sendJson(res, 200, { models });
    log(`  ↳ /api/tags: returned ${models.length} model(s)`);
  } catch (err) {
    logErr(`/api/tags: ${err.message}`);
    sendJson(res, 502, { error: err.message });
  }
}

// GET /api/version
function handleVersion(res) {
  sendJson(res, 200, { version: "0.1.0" });
}

// POST /api/chat  →  POST /v1/chat/completions
async function handleChat(req, res) {
  const raw = await readBody(req);
  let body;
  try { body = JSON.parse(raw.toString()); } catch { return sendJson(res, 400, { error: "invalid json" }); }

  const stream = body.stream !== false; // Ollama defaults to streaming
  const openaiBody = {
    model: body.model,
    messages: body.messages,
    stream,
    ...(body.options ? mapOptions(body.options) : {}),
  };

  log(`→ POST /api/chat model=${body.model} stream=${stream}`);

  try {
    const upstream = await litellmRequest("POST", "/v1/chat/completions", openaiBody);
    log(`← ${upstream.statusCode} /api/chat`);

    if (upstream.statusCode !== 200) {
      const errBody = await readJson(upstream).catch(() => ({ error: "upstream error" }));
      logErr(`/api/chat upstream ${upstream.statusCode}: ${JSON.stringify(errBody)}`);
      return sendJson(res, upstream.statusCode, errBody);
    }

    if (stream) {
      res.writeHead(200, { "content-type": "application/x-ndjson", "transfer-encoding": "chunked" });
      let buffer = "";
      upstream.on("data", (chunk) => {
        buffer += chunk.toString();
        const lines = buffer.split("\n");
        buffer = lines.pop(); // keep incomplete line
        for (const line of lines) {
          const trimmed = line.replace(/^data:\s*/, "").trim();
          if (!trimmed || trimmed === "[DONE]") continue;
          try {
            const ev = JSON.parse(trimmed);
            const delta = ev.choices?.[0]?.delta?.content ?? "";
            const done = ev.choices?.[0]?.finish_reason != null;
            const ollamaChunk = {
              model: body.model,
              created_at: new Date().toISOString(),
              message: { role: "assistant", content: delta },
              done,
            };
            res.write(JSON.stringify(ollamaChunk) + "\n");
          } catch { /* skip malformed */ }
        }
      });
      upstream.on("end", () => {
        // ensure a final done=true frame
        res.write(JSON.stringify({ model: body.model, created_at: new Date().toISOString(), message: { role: "assistant", content: "" }, done: true }) + "\n");
        res.end();
      });
    } else {
      const data = await readJson(upstream);
      const content = data.choices?.[0]?.message?.content ?? "";
      sendJson(res, 200, {
        model: body.model,
        created_at: new Date().toISOString(),
        message: { role: "assistant", content },
        done: true,
      });
    }
  } catch (err) {
    logErr(`/api/chat: ${err.message}`);
    sendJson(res, 502, { error: err.message });
  }
}

// POST /api/generate  →  POST /v1/chat/completions (prompt as user message)
async function handleGenerate(req, res) {
  const raw = await readBody(req);
  let body;
  try { body = JSON.parse(raw.toString()); } catch { return sendJson(res, 400, { error: "invalid json" }); }

  const stream = body.stream !== false;
  const messages = [];
  if (body.system) messages.push({ role: "system", content: body.system });
  messages.push({ role: "user", content: body.prompt });

  const openaiBody = {
    model: body.model,
    messages,
    stream,
    ...(body.options ? mapOptions(body.options) : {}),
  };

  log(`→ POST /api/generate model=${body.model} stream=${stream}`);

  try {
    const upstream = await litellmRequest("POST", "/v1/chat/completions", openaiBody);
    log(`← ${upstream.statusCode} /api/generate`);

    if (upstream.statusCode !== 200) {
      const errBody = await readJson(upstream).catch(() => ({ error: "upstream error" }));
      logErr(`/api/generate upstream ${upstream.statusCode}: ${JSON.stringify(errBody)}`);
      return sendJson(res, upstream.statusCode, errBody);
    }

    if (stream) {
      res.writeHead(200, { "content-type": "application/x-ndjson", "transfer-encoding": "chunked" });
      let buffer = "";
      upstream.on("data", (chunk) => {
        buffer += chunk.toString();
        const lines = buffer.split("\n");
        buffer = lines.pop();
        for (const line of lines) {
          const trimmed = line.replace(/^data:\s*/, "").trim();
          if (!trimmed || trimmed === "[DONE]") continue;
          try {
            const ev = JSON.parse(trimmed);
            const token = ev.choices?.[0]?.delta?.content ?? "";
            const done = ev.choices?.[0]?.finish_reason != null;
            res.write(JSON.stringify({ model: body.model, created_at: new Date().toISOString(), response: token, done }) + "\n");
          } catch { /* skip */ }
        }
      });
      upstream.on("end", () => {
        res.write(JSON.stringify({ model: body.model, created_at: new Date().toISOString(), response: "", done: true }) + "\n");
        res.end();
      });
    } else {
      const data = await readJson(upstream);
      const response = data.choices?.[0]?.message?.content ?? "";
      sendJson(res, 200, { model: body.model, created_at: new Date().toISOString(), response, done: true });
    }
  } catch (err) {
    logErr(`/api/generate: ${err.message}`);
    sendJson(res, 502, { error: err.message });
  }
}

// Rewrite a single SSE line: promote reasoning_content → content when content is missing
function fixChunk(line) {
  const trimmed = line.replace(/^data:\s*/, "").trim();
  if (!trimmed || trimmed === "[DONE]") return line;
  try {
    const ev = JSON.parse(trimmed);
    let changed = false;
    for (const choice of ev.choices ?? []) {
      const delta = choice.delta;
      if (delta && delta.reasoning_content != null && !delta.content) {
        delta.content = delta.reasoning_content;
        delete delta.reasoning_content;
        changed = true;
      }
    }
    return changed ? "data: " + JSON.stringify(ev) : line;
  } catch {
    return line;
  }
}

// Generic passthrough for everything else — rewrites DeepSeek reasoning_content → content
function handlePassthrough(req, res, bodyBuf) {
  const headers = { ...req.headers, authorization: `Bearer ${AUTH_KEY}`, host: `${TARGET_HOST}:${TARGET_PORT}` };
  if (bodyBuf?.length) headers["content-length"] = bodyBuf.length;

  // Log a summary of the outgoing request
  let reqSummary = `→ ${req.method} ${req.url}`;
  try {
    const parsed = JSON.parse(bodyBuf.toString());
    if (parsed.model) reqSummary += ` model=${parsed.model} stream=${parsed.stream}`;
  } catch { /* not json */ }
  log(reqSummary);

  const proxyReq = http.request(
    { hostname: TARGET_HOST, port: TARGET_PORT, path: req.url, method: req.method, headers },
    (proxyRes) => {
      const status = proxyRes.statusCode;
      const isStream = (proxyRes.headers["content-type"] ?? "").includes("event-stream");
      log(`← ${status} ${req.url}${isStream ? " (stream)" : ""}`);

      res.writeHead(status, proxyRes.headers);

      if (status !== 200) {
        // Buffer and log the full error body
        const errChunks = [];
        proxyRes.on("data", c => errChunks.push(c));
        proxyRes.on("end", () => {
          const errBody = Buffer.concat(errChunks);
          logErr(`upstream error body: ${errBody.toString().slice(0, 1000)}`);
          res.end(errBody);
        });
        return;
      }

      if (!isStream) {
        proxyRes.pipe(res, { end: true });
        return;
      }

      // Stream: rewrite chunks line by line
      let buffer = "";
      let chunkCount = 0;
      let hadReasoningContent = false;
      proxyRes.on("data", (chunk) => {
        buffer += chunk.toString();
        const lines = buffer.split("\n");
        buffer = lines.pop();
        for (const line of lines) {
          const fixed = fixChunk(line);
          if (fixed !== line) hadReasoningContent = true;
          res.write(fixed + "\n");
          chunkCount++;
        }
      });
      proxyRes.on("end", () => {
        if (buffer) res.write(fixChunk(buffer) + "\n");
        res.end();
        log(`  stream done: ${chunkCount} chunks${hadReasoningContent ? " [rewrote reasoning_content→content]" : ""}`);
      });
    }
  );
  proxyReq.on("error", (err) => {
    logErr(`passthrough error ${req.url}: ${err.message}`);
    if (!res.headersSent) res.writeHead(502);
    res.end(`Bad Gateway: ${err.message}`);
  });
  if (bodyBuf?.length) proxyReq.write(bodyBuf);
  proxyReq.end();
}

// Map Ollama options → OpenAI parameters
function mapOptions(opts) {
  const out = {};
  if (opts.temperature != null) out.temperature = opts.temperature;
  if (opts.top_p != null) out.top_p = opts.top_p;
  if (opts.num_predict != null) out.max_tokens = opts.num_predict;
  if (opts.stop != null) out.stop = opts.stop;
  return out;
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

const server = http.createServer(async (req, res) => {
  const { method, url } = req;

  if (method === "GET" && url === "/api/tags") return handleTags(res);
  if (method === "GET" && (url === "/api/version" || url === "/")) return handleVersion(res);
  if (method === "POST" && url === "/api/chat") return handleChat(req, res);
  if (method === "POST" && url === "/api/generate") return handleGenerate(req, res);

  // passthrough (includes /v1/*)
  const bodyBuf = await readBody(req);
  handlePassthrough(req, res, bodyBuf);
});

server.listen(LISTEN_PORT, LISTEN_HOST, () => {
  log(`LLM proxy started  http://${LISTEN_HOST}:${LISTEN_PORT}  →  http://${TARGET_HOST}:${TARGET_PORT}`);
  log(`Log file: ${LOG_FILE}`);
});

server.on("error", (err) => {
  if (err.code === "EADDRINUSE") {
    logErr(`Port ${LISTEN_PORT} already in use. Stop Ollama: launchctl stop com.ollama.ollama`);
  } else {
    logErr(err.message);
  }
  process.exit(1);
});
