#!/usr/bin/env python3
"""Fix: remove leftover initMsg declaration before the for loop in sendRegister."""
import sys

path = sys.argv[1]
with open(path, 'r') as f:
    content = f.read()

# Remove the leftover initMsg line before the for loop
wrong = '''func (s *MCPSession) sendRegister() {\n\t// Request tool list from MCP server to register with relay\n\tinitMsg := json.RawMessage(`{"jsonrpc":"2.0","id":0,"method":"tools/list","params":{}}`)\n\tfor i := 0; i < 15; i++ {'''
right = '''func (s *MCPSession) sendRegister() {\n\t// Poll tools/list until we get non-empty tools, or timeout (max 15s)\n\tfor i := 0; i < 15; i++ {'''

if wrong in content:
    content = content.replace(wrong, right, 1)
    print("Fixed leftover initMsg declaration!")
else:
    print("Pattern not found. Debugging...")
    idx = content.find('sendRegister()')
    if idx >= 0:
        print(repr(content[idx:idx+300]))

with open(path, 'w') as f:
    f.write(content)
