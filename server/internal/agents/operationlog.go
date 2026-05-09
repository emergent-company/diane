// Package agents provides the agent definition system.
//
// This file implements the OperationLog audit trail — system operations
// are recorded as graph objects of type OperationLog in the system namespace.
// All Diane nodes write to this log, providing cross-instance observability.
package agents

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// OperationLog holds a single audit log entry.
type OperationLog struct {
	Op        string `json:"op"`                  // e.g. "agent.delete", "agent.seed"
	Target    string `json:"target"`              // what was acted on (agent name, MCP server name)
	Actor     string `json:"actor"`               // "cli", "local-api", "watch", "user:{name}"
	Status    string `json:"status"`              // "success", "failure", "partial"
	Detail    string `json:"detail"`              // human-readable description
	Node      string `json:"node"`                // hostname that performed the op
}

// WriteOperationLog creates an OperationLog entity in the graph.
// Returns the created object or an error.
func WriteOperationLog(ctx context.Context, graphClient *graph.Client, entry *OperationLog) error {
	if graphClient == nil {
		log.Printf("[operation-log] skipped (no graph client): op=%s target=%s", entry.Op, entry.Target)
		return nil
	}

	// Auto-fill node if empty
	node := entry.Node
	if node == "" {
		hostname, err := os.Hostname()
		if err != nil {
			node = "unknown"
		} else {
			node = hostname
		}
	}

	_, err := graphClient.CreateObject(ctx, &graph.CreateObjectRequest{
		Type: "OperationLog",
		Properties: map[string]any{
			"op":     entry.Op,
			"target": entry.Target,
			"actor":  entry.Actor,
			"status": entry.Status,
			"detail": entry.Detail,
			"node":   node,
		},
	})
	if err != nil {
		return fmt.Errorf("write OperationLog: %w", err)
	}

	log.Printf("[operation-log] wrote: op=%s target=%s actor=%s status=%s",
		entry.Op, entry.Target, entry.Actor, entry.Status)
	return nil
}

// DefaultOperationLog returns a fresh OperationLog with default fields.
// Fields that vary per call (Op, Target, Status, Detail) are left empty.
func DefaultOperationLog() *OperationLog {
	hostname, _ := os.Hostname()
	return &OperationLog{
		Node: hostname,
	}
}

// WriteAgentOp is a convenience wrapper that logs an agent operation.
// It constructs the OperationLog and writes it in one call.
func WriteAgentOp(ctx context.Context, graphClient *graph.Client, op string, agentName string, actor string, status string, detail string) error {
	return WriteOperationLog(ctx, graphClient, &OperationLog{
		Op:     op,
		Target: agentName,
		Actor:  actor,
		Status: status,
		Detail: detail,
		Node:   "",
	})
}

// Now holds the current time for log message purposes.
// It's intentionally unused in the struct — timestamps come from
// the graph object's creation time.
var _ = time.Now
