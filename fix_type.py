#!/usr/bin/env python3
"""Fix Go type issue in hasTools."""
import sys

path = sys.argv[1]
with open(path, 'r') as f:
    content = f.read()

old = '''// hasTools checks if a tools/list response contains AirMCP tools.
// We specifically wait for airmcp_* tools since other MCP servers (brightdata,
// devtools) start faster and would cause registration before AirMCP is ready.
func (s *MCPSession) hasTools(data []byte) bool {
\tvar resp struct {
\t\tResult *struct {
\t\t\tTools []struct {
\t\t\t\tName string `json:"name"`
\t\t\t} `json:"tools"`
\t\t} `json:"result,omitempty"`
\t\tTools []struct {
\t\t\tName string `json:"name"`
\t\t} `json:"tools,omitempty"`
\t}
\tif err := json.Unmarshal(data, &resp); err != nil {
\t\treturn false // parse error, wait for next poll
\t}
\tvar tools []struct{ Name string }
\tif resp.Result != nil {
\t\ttools = resp.Result.Tools
\t} else {
\t\ttools = resp.Tools
\t}
\t// Wait for at least one airmcp_* tool to be present
\tfor _, t := range tools {
\t\tif strings.Contains(t.Name, "airmcp") {
\t\t\treturn true
\t\t}
\t}
\treturn false
}'''

new = '''// hasTools checks if a tools/list response contains AirMCP tools.
// We specifically wait for airmcp_* tools since other MCP servers (brightdata,
// devtools) start faster and would cause registration before AirMCP is ready.
func (s *MCPSession) hasTools(data []byte) bool {
\tvar resp struct {
\t\tResult *struct {
\t\t\tTools []struct {
\t\t\t\tName string `json:"name"`
\t\t\t} `json:"tools"`
\t\t} `json:"result,omitempty"`
\t\tTools []struct {
\t\t\tName string `json:"name"`
\t\t} `json:"tools,omitempty"`
\t}
\tif err := json.Unmarshal(data, &resp); err != nil {
\t\treturn false // parse error, wait for next poll
\t}
\t// Check Result wrapper first
\tif resp.Result != nil {
\t\tfor _, t := range resp.Result.Tools {
\t\t\tif strings.Contains(t.Name, "airmcp") {
\t\t\t\treturn true
\t\t\t}
\t\t}
\t}
\t// Check flat format
\tfor _, t := range resp.Tools {
\t\tif strings.Contains(t.Name, "airmcp") {
\t\t\treturn true
\t\t}
\t}
\treturn false
}'''

if old in content:
    content = content.replace(old, new, 1)
    print("Fixed hasTools type issue - inline iteration")
else:
    print("WARN: pattern not found")

with open(path, 'w') as f:
    f.write(content)
print("Done")
