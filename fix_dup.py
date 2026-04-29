#!/usr/bin/env python3
"""Fix the duplicate initMsg in sendRegister."""
import sys

path = sys.argv[1] if len(sys.argv) > 1 else "server/cmd/diane/mcp_relay.go"

with open(path, "r") as f:
    content = f.read()

# Remove the duplicate block between sendRegister() and the for loop
# It has the original initMsg write+flush before the polling for loop
old_dup = '''func (s *MCPSession) sendRegister() {
\t// Request tool list from MCP server to register with relay
\tinitMsg := json.RawMessage(`{"jsonrpc":"2.0","id":0,"method":"tools/list","params":{}}`)
\ts.mcpIn.Write(initMsg)
\ts.mcpIn.WriteByte('\\n')
\ts.mcpIn.Flush()

\tfor i := 0; i < 15; i++ {
\t\tinitMsg := json.RawMessage(`{"jsonrpc":"2.0","id":0,"method":"tools/list","params":{}}`)'''

new_good = '''func (s *MCPSession) sendRegister() {
\tfor i := 0; i < 15; i++ {
\t\tinitMsg := json.RawMessage(`{"jsonrpc":"2.0","id":0,"method":"tools/list","params":{}}`)'''

if old_dup in content:
    content = content.replace(old_dup, new_good, 1)
    print("Fixed duplicate initMsg in sendRegister!")
else:
    print("Target pattern not found. Debugging...")
    idx = content.find("sendRegister")
    if idx >= 0:
        print(repr(content[idx:idx+400]))

with open(path, "w") as f:
    f.write(content)
