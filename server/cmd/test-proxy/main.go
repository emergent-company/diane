package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	fmt.Println("=== Creating Proxy ===")
	proxy, err := mcpproxy.NewProxy([]mcpproxy.ServerConfig{
		{
			Name:    "echo-server",
			Enabled: true,
			Type:    "stdio",
			Command: "/tmp/mcp-relay-test/echo-mcp",
			Args:    []string{},
			Env:     map[string]string{},
		},
	})
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
