#!/usr/bin/env python3
"""Fix hasTools to wait for AirMCP tools specifically, not just ANY tools."""
import sys

path = sys.argv[1]
with open(path, 'r') as f:
    content = f.read()

# Replace hasTools with a version that checks for airmcp tools specifically
old_has = '''// hasTools checks if a tools/list response contains any tools.
func (s *MCPSession) hasTools(data []byte) bool {
\tvar resp struct {
\t\tResult *struct {
\t\t\tTools []any `json:"tools"`
\t\t} `json:"result,omitempty"`
\t\tTools []any `json:"tools,omitempty"`
\t}
\tif err := json.Unmarshal(data, &resp); err != nil {
\t\treturn true // parse error, assume tools present
\t}
\tif resp.Result != nil {
\t\treturn len(resp.Result.Tools) > 0
\t}
\treturn len(resp.Tools) > 0
}'''

new_has = '''// hasTools checks if a tools/list response contains AirMCP tools.
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

if old_has in content:
    content = content.replace(old_has, new_has, 1)
    print("Fixed hasTools to check for airmcp prefix")
else:
    print("WARN: old hasTools pattern not found, trying alternate...")
    idx = content.find('hasTools checks')
    if idx >= 0:
        print(repr(content[idx:idx+400]))
    # Also check if new version already in place
    if 'airmcp' in content:
        print("hasTools already has airmcp check, skipping")

# Also add "strings" import if it's missing
if 'strings.Contains' in content and '"strings"' not in content:
    # Need to add strings import
    import_section = 'import (\n'
    if import_section in content:
        content = content.replace(import_section, 'import (\n\t"strings"\n', 1)
        print("Added strings import")
    else:
        # Check if the file uses grouped imports
        if 'import (\n\t"bufio"\n' in content:
            content = content.replace('import (\n\t"bufio"\n', 'import (\n\t"bufio"\n\t"strings"\n', 1)
            print("Added strings import in existing group")
        else:
            print("WARN: could not find import section for strings")

with open(path, 'w') as f:
    f.write(content)
print("Done")
