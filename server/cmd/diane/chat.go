package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Emergent-Comapny/diane/internal/config"
)

func cmdChat(args []string) {
	if len(args) == 0 {
		// Interactive REPL mode
		runChatREPL("diane-default")
		return
	}

	// Parse flags
	var agentName string
	var sessionID string

	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--agent" && i+1 < len(args):
			agentName = args[i+1]
			i++
		case args[i] == "--session" && i+1 < len(args):
			sessionID = args[i+1]
			i++
		case args[i] == "--help" || args[i] == "-h":
			fmt.Println("Usage: diane chat [message] [--agent <agent>] [--session <session>]")
			fmt.Println()
			fmt.Println("Send a message to an agent via ACP streaming and display the response.")
			fmt.Println()
			fmt.Println("If no message is given, enters interactive REPL mode.")
			fmt.Println()
			fmt.Println("Flags:")
			fmt.Println("  --agent <name>     Agent to chat with (default: diane-default)")
			fmt.Println("  --session <id>     Reuse an existing ACP session ID")
			fmt.Println()
			fmt.Println("Examples:")
			fmt.Println("  diane chat 'Hello, what agents do you have?'")
			fmt.Println("  diane chat --agent diane-codebase 'analyze this'")
			fmt.Println("  diane chat  (starts interactive REPL)")
			return
		default:
			remaining = append(remaining, args[i])
		}
	}

	if agentName == "" {
		agentName = "diane-default"
	}

	if len(remaining) == 0 {
		// Interactive mode
		runChatREPL(agentName)
		return
	}

	// One-shot mode
	message := strings.Join(remaining, " ")
	runChatOneShot(agentName, sessionID, message)
}

func runChatREPL(agentName string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	proj := cfg.Active()
	if proj == nil {
		fmt.Fprintf(os.Stderr, "error: no active project configured. Run 'diane init' first.\n")
		os.Exit(1)
	}

	serverURL := strings.TrimRight(proj.ServerURL, "/")
	apiKey := proj.Token

	// Create ACP session
	sessionID, err := createACPSession(serverURL, apiKey, agentName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating session: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Session: %s\n", sessionID)
	fmt.Println("Interactive chat (Ctrl+C to exit):")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println()
				return
			}
			fmt.Fprintf(os.Stderr, "read error: %v\n", err)
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "/exit" || line == "/quit" {
			return
		}

		streamACP(serverURL, apiKey, agentName, sessionID, line, os.Stdout)
		fmt.Println()
	}
}

func runChatOneShot(agentName, sessionID, message string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	proj := cfg.Active()
	if proj == nil {
		fmt.Fprintf(os.Stderr, "error: no active project configured. Run 'diane init' first.\n")
		os.Exit(1)
	}

	serverURL := strings.TrimRight(proj.ServerURL, "/")
	apiKey := proj.Token

	if sessionID == "" {
		sid, err := createACPSession(serverURL, apiKey, agentName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating session: %v\n", err)
			os.Exit(1)
		}
		sessionID = sid
	}

	streamACP(serverURL, apiKey, agentName, sessionID, message, os.Stdout)
	fmt.Println()
}

type acpSessionResponse struct {
	ID string `json:"id"`
}

func createACPSession(serverURL, apiKey, agentName string) (string, error) {
	u := fmt.Sprintf("%s/acp/v1/sessions", serverURL)
	body, _ := json.Marshal(map[string]string{"agent_name": agentName})

	req, err := http.NewRequest("POST", u, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setACPHeaders(req, apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var session acpSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return session.ID, nil
}

func setACPHeaders(req *http.Request, apiKey string) {
	if strings.HasPrefix(apiKey, "emt_") {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		req.Header.Set("X-API-Key", apiKey)
	}
}

func streamACP(serverURL, apiKey, agentName, sessionID, message string, w io.Writer) {
	u := fmt.Sprintf("%s/acp/v1/agents/%s/runs", serverURL, url.PathEscape(agentName))

	payload := map[string]any{
		"mode":       "stream",
		"session_id": sessionID,
		"message": []map[string]string{
			{"content_type": "text/plain", "content": message},
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", u, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	setACPHeaders(req, apiKey)

	client := &http.Client{Timeout: 0} // no timeout for streaming
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: acp %d: %s\n", resp.StatusCode, strings.TrimSpace(string(respBody)))
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	var currentEventType string
	var toolCallCount int
	var textOutput strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")
			if dataStr == "[DONE]" {
				break
			}

			var rawData map[string]any
			if err := json.Unmarshal([]byte(dataStr), &rawData); err != nil {
				continue
			}

			eventType := currentEventType
			currentEventType = ""

			switch eventType {
			case "message.part":
				part, ok := rawData["part"].(map[string]any)
				if !ok {
					continue
				}
				contentType, _ := part["content_type"].(string)
				switch contentType {
				case "text/plain":
					content, _ := part["content"].(string)
					if content != "" {
						textOutput.WriteString(content)
						fmt.Fprint(w, content)
					}
				case "application/json":
					if meta, ok := part["metadata"].(map[string]any); ok {
						kind, _ := meta["kind"].(string)
						if kind == "trajectory" {
							toolName, _ := meta["tool_name"].(string)
							if _, hasOutput := meta["tool_output"]; hasOutput {
								toolCallCount++
							} else {
								toolCallCount++
								fmt.Fprintf(w, "\n[Tool: %s]\n", toolName)
							}
						}
					}
				}

			case "run.completed":
				// Stream done
				if textOutput.Len() == 0 && toolCallCount > 0 {
					fmt.Fprintf(w, "\n[Done — %d tool call(s), no text response]\n", toolCallCount)
				} else if textOutput.Len() > 0 {
					fmt.Fprintln(w)
				}

			case "run.failed", "run.cancelled":
				errMsg := "run " + strings.TrimPrefix(eventType, "run.")
				if run, ok := rawData["run"].(map[string]any); ok {
					if e, ok := run["error"].(map[string]any); ok {
						if m, ok := e["message"].(string); ok {
							errMsg = m
						}
					}
				}
				fmt.Fprintf(os.Stderr, "\n[Error: %s]\n", errMsg)

			case "error":
				errMsg := "stream error"
				if e, ok := rawData["error"].(map[string]any); ok {
					if m, ok := e["message"].(string); ok {
						errMsg = m
					}
				}
				fmt.Fprintf(os.Stderr, "\n[Error: %s]\n", errMsg)
			}

			// Break on completion/failure
			if eventType == "run.completed" || eventType == "run.failed" ||
				eventType == "run.cancelled" || eventType == "error" {
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "\n[Scanner error: %v]\n", err)
	}
}
