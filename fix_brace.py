#!/usr/bin/env python3
"""Fix the extra closing brace from the backoff reset insertion."""
import sys

path = sys.argv[1]
with open(path, 'r') as f:
    content = f.read()

# The pattern: "}\n\t\t}\n\t\t} else {" -> "}\n\t\t} else {"
# Fix: remove the extra "}" between the if-block close and "} else {"
wrong = '\t\tif err != nil {\n\t\t\tlog.Printf("[mcp-relay] Connection error: %v (reconnecting in %v)", err, backoff)\n\t\t}\n\t\t} else {\n\t\t\tbackoff = cfg.ReconnectDelay\n\t\t}'
right = '\t\tif err != nil {\n\t\t\tlog.Printf("[mcp-relay] Connection error: %v (reconnecting in %v)", err, backoff)\n\t\t} else {\n\t\t\tbackoff = cfg.ReconnectDelay\n\t\t}'

if wrong in content:
    content = content.replace(wrong, right, 1)
    print("Fixed extra closing brace!")
else:
    print("Pattern not found. Debugging...")
    idx = content.find('if err != nil')
    if idx >= 0:
        print(repr(content[idx:idx+300]))

with open(path, 'w') as f:
    f.write(content)
