#!/usr/bin/env python3
"""Fix: declare toolsResp before the for loop in sendRegister, use = not :="""
import sys

path = sys.argv[1]
with open(path, 'r') as f:
    content = f.read()

# Fix 1: declare toolsResp before the for loop
old1 = "func (s *MCPSession) sendRegister() {\n\t// Poll tools/list until we get non-empty tools, or timeout (max 15s)\n\tfor i := 0; i < 15; i++ {"
new1 = "func (s *MCPSession) sendRegister() {\n\t// Poll tools/list until we get non-empty tools, or timeout (max 15s)\n\tvar toolsResp []byte\n\tfor i := 0; i < 15; i++ {"

if old1 in content:
    content = content.replace(old1, new1, 1)
    print("Fix 1: declared toolsResp before loop")
else:
    print("WARN: Fix 1 pattern not found")
    idx = content.find("sendRegister()")
    if idx >= 0:
        print(repr(content[idx:idx+200]))

# Fix 2: change ":=" to "=" for toolsResp inside the loop
# "toolsResp = line" — already using "=" from the fix script
# "toolsResp = s.mcpOut.Bytes()" — already using "="
# These should be fine since toolsResp is now declared with var above

with open(path, 'w') as f:
    f.write(content)
print("Done")
