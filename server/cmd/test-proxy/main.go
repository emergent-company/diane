package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	configPath := "/tmp/mcp-relay-test/test-proxy-config.json"
	config := map[string]interface{}{
		"servers": []map[string]interface{}{
			{
				"name":    "echo-server",
				"enabled": true,
				"type":    "stdio",
				"command": "/tmp/mcp-relay-test/echo-mcp",
				"args":    []string{},
				"env":     map[string]string{},
			},
		},
	}
	configData, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(configPath, configData, 0644)

	fmt.Println("=== Creating Proxy ===")
	proxy, err := mcpproxy.NewProxy(configPath)
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}
	defer proxy.Close()
	fmt.Println("Proxy created successfully")

	fmt.Println("=== Listing tools (with timeout) ===")
	done := make(chan struct{})
	var tools []map[string]interface{}
	var listErr error

	go func() {
		tools, listErr = proxy.ListAllTools()
		close(done)
	}()

	select {
	case <-done:
		if listErr != nil {
			log.Fatalf("ListAllTools error: %v", listErr)
		}
		fmt.Printf("Found %d tools:\n", len(tools))
		for _, t := range tools {
			fmt.Printf("  - %s\n", t["name"])
		}
	case <-time.After(10 * time.Second):
		log.Fatal("TIMEOUT: ListAllTools hung!")
	}

	fmt.Println("=== Calling echo-server_echo_text ===")
	done2 := make(chan struct{})
	var callResult json.RawMessage
	var callErr error

	go func() {
		callResult, callErr = proxy.CallTool("echo-server_echo_text", map[string]interface{}{
			"text": "Hello from proxy test!",
		})
		close(done2)
	}()

	select {
	case <-done2:
		if callErr != nil {
			log.Fatalf("CallTool error: %v", callErr)
		}
		fmt.Printf("Result: %s\n", string(callResult))
	case <-time.After(10 * time.Second):
		log.Fatal("TIMEOUT: CallTool hung!")
	}

	fmt.Println("\n=== All mcpproxy tests passed ===")
}
