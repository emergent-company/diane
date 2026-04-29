// Command: diane tool test
//
// Tests an MCP tool by calling it directly through the MCP proxy with a
// configurable timeout. This catches hanging tools (e.g., AirMCP tools that
// block waiting for upstream services) without blocking the agent runner.
//
// Usage:
//
//	diane tool test <tool_name> [--timeout <duration>] [--arg key=value ...]
//
// Examples:
//
//	# Test a specific tool with default 30s timeout
//	diane tool test airmcp_list_reminder_lists
//
//	# Test with arguments and custom timeout
//	diane tool test airmcp_list_reminders --timeout 10s --arg list_id=123
//
//	# Test a non-AirMCP tool
//	diane tool test filesystem_read_file --timeout 5s --arg path=/tmp/test.txt

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

func cmdToolTest(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: diane tool test <tool_name> [--timeout <duration>] [--arg key=value ...]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Tests an MCP tool by calling it through the proxy with a timeout.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fmt.Fprintln(os.Stderr, "  --timeout     Timeout for the tool call (default: 30s)")
		fmt.Fprintln(os.Stderr, "  --arg         Tool arguments in key=value format (repeatable)")
		fmt.Fprintln(os.Stderr, "  --json        Arguments as a JSON string")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("tool-test", flag.ExitOnError)
	timeoutStr := fs.String("timeout", "30s", "Timeout for the tool call")
	argsRaw := fs.String("arg", "", "Tool argument in key=value format (repeatable)")
	jsonArgs := fs.String("json", "", "Tool arguments as JSON string")
	_ = fs.Parse(args[1:])

	toolName := args[0]

	// Parse timeout
	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		log.Fatalf("Invalid timeout: %v", err)
	}

	// Parse arguments
	var arguments map[string]interface{}
	if *jsonArgs != "" {
		// JSON mode: parse the full arguments object
		if err := json.Unmarshal([]byte(*jsonArgs), &arguments); err != nil {
			log.Fatalf("Invalid JSON arguments: %v", err)
		}
	} else {
		// key=value mode: parse multiple --arg flags
		arguments = make(map[string]interface{})
		for _, a := range strings.Split(*argsRaw, ",") {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			parts := strings.SplitN(a, "=", 2)
			if len(parts) != 2 {
				log.Fatalf("Invalid argument format: %s (expected key=value)", a)
			}
			arguments[parts[0]] = parts[1]
		}
	}

	if !jsonOutput {
		fmt.Printf("🔧 Testing tool: %s\n", toolName)
		fmt.Printf("   Timeout:     %v\n", timeout)
		if len(arguments) > 0 {
			fmt.Printf("   Arguments:   %v\n", arguments)
		} else {
			fmt.Printf("   Arguments:   (none)\n")
		}
		fmt.Println()
	}

	// Initialize MCP proxy (same config as diane mcp serve)
	configPath := mcpproxy.GetDefaultConfigPath()
	proxy, err := mcpproxy.NewProxy(configPath)
	if err != nil {
		if jsonOutput {
			emitJSON("error", map[string]string{"message": "Failed to initialize MCP proxy: " + err.Error()})
		} else {
			log.Fatalf("Failed to initialize MCP proxy: %v", err)
		}
		return
	}
	defer proxy.Close()

	if !jsonOutput {
		fmt.Printf("⏳ Calling tool... (timeout: %v)\n", timeout)
	}

	start := time.Now()

	// Wrap in a channel for timeout
	type result struct {
		output json.RawMessage
		err    error
	}

	resultCh := make(chan result, 1)
	go func() {
		output, err := proxy.CallTool(toolName, arguments)
		resultCh <- result{output, err}
	}()

	select {
	case res := <-resultCh:
		elapsed := time.Since(start)
		if res.err != nil {
			if jsonOutput {
				emitJSON("error", map[string]interface{}{
					"tool":        toolName,
					"duration_ms": elapsed.Milliseconds(),
					"error":       res.err.Error(),
				})
			} else {
				fmt.Printf("❌ Tool call failed after %v:\n", elapsed)
				fmt.Printf("   Error: %v\n", res.err)
			}
			os.Exit(1)
		}

		if jsonOutput {
			var resultObj interface{}
			if err := json.Unmarshal(res.output, &resultObj); err != nil {
				resultObj = string(res.output)
			}
			emitJSON("ok", map[string]interface{}{
				"tool":        toolName,
				"duration_ms": elapsed.Milliseconds(),
				"result":      resultObj,
			})
		} else {
			fmt.Printf("✅ Tool call succeeded in %v\n", elapsed)
			var prettyResult interface{}
			if err := json.Unmarshal(res.output, &prettyResult); err == nil {
				pretty, _ := json.MarshalIndent(prettyResult, "   ", "  ")
				fmt.Printf("   Result:\n")
				fmt.Printf("   %s\n", string(pretty))
			} else {
				fmt.Printf("   Result: %s\n", string(res.output))
			}
		}

	case <-time.After(timeout):
		elapsed := time.Since(start)
		if jsonOutput {
			emitJSON("error", map[string]interface{}{
				"tool":        toolName,
				"duration_ms": elapsed.Milliseconds(),
				"error":       "timeout",
				"timeout":     timeout.String(),
			})
		} else {
			fmt.Printf("❌ Tool call TIMED OUT after %v\n", elapsed)
			fmt.Printf("   The downstream service is hanging and needs investigation.\n")
			fmt.Printf("   Try a shorter --timeout or check the upstream service health.\n")
		}
		os.Exit(1)
	}
}
