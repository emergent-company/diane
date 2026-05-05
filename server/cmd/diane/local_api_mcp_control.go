// Package: main
// MCP server save/toggle/delete handlers.
package main

import (
	"net/http"
	"strings"
)

func (a *localAPIServer) handleMCPToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	serverName := strings.TrimPrefix(r.URL.Path, "/api/mcp-servers/toggle/")
	if serverName == "" {
		jsonError(w, http.StatusBadRequest, "server name required")
		return
	}

	// MCP server configurations are managed through the Memory Platform graph.
	// Toggle the server's enabled state via the graph API.
	jsonError(w, http.StatusNotImplemented, "MCP server management is now handled via the Memory Platform graph")
}

// POST /api/mcp-servers/store — add or update an MCP server
func (a *localAPIServer) handleMCPSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// MCP server configurations are managed through the Memory Platform graph.
	// Use 'diane mcp add' or the dashboard to manage servers.
	jsonError(w, http.StatusNotImplemented, "MCP server management is now handled via the Memory Platform graph")
}

// DELETE /api/mcp-servers/delete/{name} — remove an MCP server
func (a *localAPIServer) handleMCPDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// MCP server configurations are managed through the Memory Platform graph.
	// Use 'diane mcp add' or the dashboard to manage servers.
	jsonError(w, http.StatusNotImplemented, "MCP server management is now handled via the Memory Platform graph")
}

// jsonResponse writes a JSON response.
