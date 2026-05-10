package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "time"

    "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
)

func main() {
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
    
    client.SetContext("", "e56b176b-a781-4670-8450-62a950650fb8")
    
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    
    // List agent definitions
    resp, err := client.AgentDefinitions.List(ctx)
    if err != nil {
        fmt.Fprintf(os.Stderr, "List agents: %v\n", err)
        os.Exit(1)
    }
    
    for _, def := range resp.Data {
        if def.Name == "diane-default" {
            b, _ := json.MarshalIndent(def, "", "  ")
            fmt.Println(string(b))
            return
        }
    }
    
    fmt.Println("diane-default not found in agent definitions")
    fmt.Printf("Found %d agent definitions\n", len(resp.Data))
    for _, d := range resp.Data {
        fmt.Printf("  - %s\n", d.Name)
    }
}
