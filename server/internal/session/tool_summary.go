package session

import (
	"fmt"
	"strings"
)

// SummarizeToolResult creates an informative 1-line summary of a tool call + result.
// Ported from Hermes' _summarize_tool_result().
// Returns strings like:
//   [terminal] ran `npm test` -> exit 0, 47 lines output
//   [read_file] read config.py from line 1 (1,200 chars)
//   [search_files] content search for 'compress' in agent/ -> 12 matches
func SummarizeToolResult(toolName, toolArgs, content string) string {
	args := parseArgs(toolArgs)
	contentLen := len(content)
	lineCount := 0
	if strings.TrimSpace(content) != "" {
		lineCount = strings.Count(content, "\n") + 1
	}

	switch toolName {
	case "terminal":
		cmd := truncateStr(args.String("command", ""), 80)
		exitCode := extractExitCode(content)
		return fmt.Sprintf("[terminal] ran `%s` -> exit %s, %d lines output", cmd, exitCode, lineCount)

	case "read_file":
		path := args.String("path", "?")
		offset := args.Int("offset", 1)
		return fmt.Sprintf("[read_file] read %s from line %d (%d chars)", path, offset, contentLen)

	case "write_file":
		path := args.String("path", "?")
		c := args.String("content", "")
		lines := strings.Count(c, "\n") + 1
		return fmt.Sprintf("[write_file] wrote to %s (%d lines)", path, lines)

	case "search_files":
		pattern := args.String("pattern", "?")
		path := args.String("path", ".")
		target := args.String("target", "content")
		count := extractJSONInt(content, "total_count")
		return fmt.Sprintf("[search_files] %s search for '%s' in %s -> %d matches", target, pattern, path, count)

	case "patch":
		path := args.String("path", "?")
		mode := args.String("mode", "replace")
		return fmt.Sprintf("[patch] %s in %s (%d chars result)", mode, path, contentLen)

	case "browser_navigate", "browser_click", "browser_snapshot",
		"browser_type", "browser_scroll", "browser_vision":
		url := args.String("url", "")
		ref := args.String("ref", "")
		detail := ""
		if url != "" {
			detail = " " + url
		} else if ref != "" {
			detail = " ref=" + ref
		}
		return fmt.Sprintf("[%s]%s (%d chars)", toolName, detail, contentLen)

	case "web_search":
		query := args.String("query", "?")
		return fmt.Sprintf("[web_search] query='%s' (%d chars result)", query, contentLen)

	case "web_extract":
		urls := args.StringSlice("urls")
		urlDesc := "?"
		if len(urls) > 0 {
			urlDesc = urls[0]
			if len(urls) > 1 {
				urlDesc += fmt.Sprintf(" (+%d more)", len(urls)-1)
			}
		}
		return fmt.Sprintf("[web_extract] %s (%d chars)", urlDesc, contentLen)

	case "delegate_task":
		goal := truncateStr(args.String("goal", ""), 60)
		return fmt.Sprintf("[delegate_task] '%s' (%d chars result)", goal, contentLen)

	case "execute_code":
		code := args.String("code", "")
		preview := truncateStr(strings.ReplaceAll(code, "\n", " "), 60)
		return fmt.Sprintf("[execute_code] `%s` (%d lines output)", preview, lineCount)

	case "skill_view", "skills_list", "skill_manage":
		name := args.String("name", "?")
		return fmt.Sprintf("[%s] name=%s (%d chars)", toolName, name, contentLen)

	case "vision_analyze":
		question := truncateStr(args.String("question", ""), 50)
		return fmt.Sprintf("[vision_analyze] '%s' (%d chars)", question, contentLen)

	case "memory":
		action := args.String("action", "?")
		target := args.String("target", "?")
		return fmt.Sprintf("[memory] %s on %s", action, target)

	case "todo":
		return "[todo] updated task list"

	case "clarify":
		return "[clarify] asked user a question"

	case "text_to_speech":
		return fmt.Sprintf("[text_to_speech] generated audio (%d chars)", contentLen)

	case "cronjob":
		action := args.String("action", "?")
		return fmt.Sprintf("[cronjob] %s", action)

	case "process":
		action := args.String("action", "?")
		sid := args.String("session_id", "?")
		return fmt.Sprintf("[process] %s session=%s", action, sid)
	}

	// Generic fallback
	firstArg := ""
	for k, v := range args {
		if len(firstArg) > 80 {
			break
		}
		sv := truncateStr(fmt.Sprintf("%v", v), 40)
		firstArg += fmt.Sprintf(" %s=%s", k, sv)
	}
	return fmt.Sprintf("[%s]%s (%d chars result)", toolName, firstArg, contentLen)
}

// PrunedToolPlaceholder is used when replacing old tool outputs.
const PrunedToolPlaceholder = "[Old tool result content cleared to save context space]"

// TruncateToolCallArgsJSON shrinks long string values in tool-call arguments JSON
// while preserving JSON validity. Ported from Hermes' _truncate_tool_call_args_json().
func TruncateToolCallArgsJSON(args string, headChars int) string {
	if headChars <= 0 {
		headChars = 200
	}
	parsed, err := tryParseJSON(args)
	if err != nil {
		return args
	}

	shrunken := shrinkStringsInValue(parsed, headChars)
	result, err := marshalJSON(shrunken)
	if err != nil {
		return args
	}
	return result
}

// truncateStr truncates a string to maxLen with an ellipsis.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
