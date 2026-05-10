//go:build ignore

package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
    "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

func main() {
    log.SetFlags(0)
    
    token := os.Getenv("MP_TOKEN")
    if token == "" {
        fmt.Fprintln(os.Stderr, "MP_TOKEN not set")
        os.Exit(1)
    }
    
    client, err := sdk.New(sdk.Config{
        ServerURL: "https://memory.emergent-company.ai",
        Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: token},
    })
    if err != nil {
        fmt.Fprintf(os.Stderr, "SDK init: %v\n", err)
        os.Exit(1)
    }
    
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    
    client.Graph.SetContext("", "e56b176b-a781-4670-8450-62a950650fb8")
    
    // Test AgentToolConfig
    fmt.Println("=== AgentToolConfig ===")
    tcResp, err := client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{Type: "AgentToolConfig", Limit: 10})
    if err != nil {
        fmt.Printf("  ERROR: %v\n", err)
    } else {
        fmt.Printf("  Items: %d\n", len(tcResp.Items))
        for _, item := range tcResp.Items {
            b, _ := json.Marshal(item)
            fmt.Printf("  - %s\n", string(b))
        }
    }
    
    fmt.Println()
    fmt.Println("=== AgentOverrideConfig ===")
    ocResp, err := client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{Type: "AgentOverrideConfig", Limit: 10})
    if err != nil {
        fmt.Printf("  ERROR: %v\n", err)
    } else {
        fmt.Printf("  Items: %d\n", len(ocResp.Items))
        for _, item := range ocResp.Items {
            b, _ := json.Marshal(item)
            fmt.Printf("  - %s\n", string(b))
        }
    }
}
