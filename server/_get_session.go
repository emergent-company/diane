package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Emergent-Comapny/diane/internal/config"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

func main() {
	sessionID := os.Args[1]

	ctx := context.Background()
	projCfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	pc := projCfg.Active()
	if pc == nil {
		fmt.Fprintf(os.Stderr, "no active project\n")
		os.Exit(1)
	}

	bridge, err := memory.New(memory.Config{
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 15 * time.Second,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bridge: %v\n", err)
		os.Exit(1)
	}
	defer bridge.Close()

	msgs, err := bridge.GetMessages(ctx, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetMessages: %v\n", err)
		os.Exit(1)
	}

	b, _ := json.MarshalIndent(msgs, "", "  ")
	fmt.Println(string(b))
}
