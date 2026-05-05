// Package: main
// Doctor/diagnostics HTTP handler.
package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"github.com/Emergent-Comapny/diane/internal/memory"
)

func (a *localAPIServer) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := context.Background()
	pc := a.config
	results := []map[string]any{}

	// 1. Config file
	results = append(results, map[string]any{
		"check":   "config_file",
		"status":  "ok",
		"message": "Config loaded",
		"details": map[string]any{
			"project_id": pc.ProjectID,
			"server_url": pc.ServerURL,
			"mode":       pc.ModeLabel(),
		},
	})

	// 2. API token present
	tokenValid := pc.Token != "" && len(pc.Token) >= 10
	tokenMsg := "Token present and valid length"
	if !tokenValid {
		tokenMsg = "Token missing or too short"
	}
	tokenStatus := "ok"
	if !tokenValid {
		tokenStatus = "error"
	}
	results = append(results, map[string]any{
		"check":   "api_token",
		"status":  tokenStatus,
		"message": tokenMsg,
	})

	if !tokenValid {
		jsonResponse(w, map[string]any{
			"ok":      false,
			"results": results,
		})
		return
	}

	// 3. SDK connection
	bridge, err := memory.New(memory.Config{
		ServerURL:         pc.ServerURL,
		APIKey:            pc.Token,
		ProjectID:         pc.ProjectID,
		OrgID:             pc.OrgID,
		HTTPClientTimeout: 10 * time.Second,
	})
	if err != nil {
		results = append(results, map[string]any{
			"check":   "sdk_connection",
			"status":  "error",
			"message": fmt.Sprintf("SDK init failed: %v", err),
		})
		jsonResponse(w, map[string]any{
			"ok":      false,
			"results": results,
		})
		return
	}
	defer bridge.Close()

	results = append(results, map[string]any{
		"check":   "sdk_connection",
		"status":  "ok",
		"message": "SDK initialized",
	})

	// 4. Project info from auth/me
	authInfo, err := bridge.GetProjectInfo(ctx)
	if err != nil {
		results = append(results, map[string]any{
			"check":   "project_info",
			"status":  "warning",
			"message": fmt.Sprintf("GetProjectInfo failed: %v", err),
		})
	} else {
		results = append(results, map[string]any{
			"check":   "project_info",
			"status":  "ok",
			"message": fmt.Sprintf("Project: %s", authInfo.ProjectName),
			"details": map[string]any{
				"project_name": authInfo.ProjectName,
			},
		})
	}

	// 5. Agent definitions count
	remoteDefs, err := bridge.ListAgentDefs(ctx)
	agentCount := 0
	if err == nil && remoteDefs != nil {
		agentCount = len(remoteDefs.Data)
	}
	agentStatus := "ok"
	agentMsg := fmt.Sprintf("%d agent(s) on MP", agentCount)
	if err != nil {
		agentStatus = "warning"
		agentMsg = fmt.Sprintf("Failed to fetch agent defs: %v", err)
	}
	results = append(results, map[string]any{
		"check":   "agent_definitions",
		"status":  agentStatus,
		"message": agentMsg,
	})

	// 6. Session CRUD
	sessionCRUD := "ok"
	sessionMsg := ""
	session, err := bridge.CreateSession(ctx, "diane-doctor-check")
	if err != nil {
		sessionCRUD = "error"
		sessionMsg = fmt.Sprintf("CreateSession: %v", err)
	} else {
		_, err = bridge.AppendMessage(ctx, session.ID, "user", "doctor test", 0)
		if err != nil {
			sessionCRUD = "error"
			sessionMsg = fmt.Sprintf("AppendMessage: %v", err)
		} else {
			_, err = bridge.GetMessages(ctx, session.ID)
			if err != nil {
				sessionCRUD = "error"
				sessionMsg = fmt.Sprintf("GetMessages: %v", err)
			} else {
				err = bridge.CloseSession(ctx, session.ID)
				if err != nil {
					sessionCRUD = "error"
					sessionMsg = fmt.Sprintf("CloseSession: %v", err)
				} else {
					sessionMsg = "Create, write, read, close — all passed"
				}
			}
		}
	}
	results = append(results, map[string]any{
		"check":   "session_crud",
		"status":  sessionCRUD,
		"message": sessionMsg,
	})

	// 7. Memory search
	searchResults, err := bridge.SearchMemory(ctx, "doctor test", 3)
	searchStatus := "ok"
	searchMsg := fmt.Sprintf("%d result(s)", len(searchResults))
	if err != nil {
		searchStatus = "warning"
		searchMsg = fmt.Sprintf("Search failed: %v", err)
	}
	results = append(results, map[string]any{
		"check":   "memory_search",
		"status":  searchStatus,
		"message": searchMsg,
	})

	// 8. Version info
	results = append(results, map[string]any{
		"check":   "server_version",
		"status":  "ok",
		"message": Version,
	})

	// 9. CLI / App version match
	appVer := readAppVersion()
	vmStatus := "ok"
	vmMsg := "Diane.app not installed"
	if appVer != "" {
		cliVer := strings.TrimPrefix(Version, "v")
		appVerClean := strings.TrimPrefix(appVer, "v")
		if cliVer == appVerClean || (cliVer == "dev" && strings.HasPrefix(appVerClean, "dev")) {
			vmStatus = "ok"
			vmMsg = fmt.Sprintf("CLI=%s, App=%s — match", Version, appVer)
		} else {
			vmStatus = "warning"
			vmMsg = fmt.Sprintf("CLI=%s, App=%s — MISMATCH", Version, appVer)
		}
	}
	results = append(results, map[string]any{
		"check":   "version_match",
		"status":  vmStatus,
		"message": vmMsg,
		"details": map[string]any{
			"cli_version": Version,
			"app_version": appVer,
		},
	})

	jsonResponse(w, map[string]any{
		"ok":      sessionCRUD == "ok",
		"version": Version,
		"results": results,
	})
}

// ─── Bug Report Handler ───────────────────────────────────

// POST /api/bugreport — accepts an error report from the companion app and creates
// a GitHub issue in the emergent-company/diane repository.
// Uses GH_TOKEN environment variable (set by gh CLI) for authentication.
