#!/usr/bin/env python3
"""Cleanly fix diane mcp relay: 
1. ReconnectDelay: 30s -> 5s
2. Reset backoff on successful reconnect
3. Poll tools/list in sendRegister until tools are ready
4. Add hasTools helper
"""
import sys

path = sys.argv[1]
with open(path, 'r') as f:
    lines = f.readlines()

# 1. Change ReconnectDelay default
for i, line in enumerate(lines):
    if 'cfg.ReconnectDelay = 30 * time.Second' in line:
        lines[i] = line.replace(
            'cfg.ReconnectDelay = 30 * time.Second',
            'cfg.ReconnectDelay = 5 * time.Second'
        )
        print(f"Change 1: Line {i+1} — reduced reconnect delay to 5s")

# 2. Change the backoff logic — add reset on success
for i, line in enumerate(lines):
    stripped = line.strip()
    if stripped == 'backoff := cfg.ReconnectDelay':
        # Look for the session.run() call a few lines later
        # and the error handling pattern
        pass

# Find the reconnect loop block: lines around "err := session.run()"
run_line = None
for i, line in enumerate(lines):
    if line.strip() == 'err := session.run()':
        run_line = i
        break

if run_line is not None:
    # The block looks like:
    #   err := session.run()
    #   if err != nil {
    #       log.Printf("[mcp-relay] Connection error: %v (reconnecting in %v)", err, backoff)
    #   }
    #   
    #   select {
    # We need to add an else branch between the if and the blank line

    # Find where the if block ends
    end_if = None
    for j in range(run_line + 1, min(run_line + 10, len(lines))):
        if lines[j].strip() == '}':
            # Check if next line is blank then select
            end_if = j
            break

    if end_if is not None:
        # Current: } [blank] select {
        # Need:   } else { [newline] backoff = cfg.ReconnectDelay [newline] } [blank] select {
        # Insert after the closing brace
        indent = lines[end_if][:len(lines[end_if]) - len(lines[end_if].lstrip())]
        lines.insert(end_if + 1, indent + '} else {\n')
        lines.insert(end_if + 2, indent + '\tbackoff = cfg.ReconnectDelay\n')
        lines.insert(end_if + 3, indent + '}\n')
        print(f"Change 2: Added backoff reset on successful reconnect")
    else:
        print("WARN: Could not find end of if block for change 2")
else:
    print("WARN: Could not find session.run() for change 2")

# 3. Modify sendRegister to poll tools/list
# Find function start
sendreg_start = None
for i, line in enumerate(lines):
    if line.strip() == 'func (s *MCPSession) sendRegister() {':
        sendreg_start = i
        break

if sendreg_start is not None:
    # Find the scan-and-read pattern
    scan_line = None
    for j in range(sendreg_start, min(sendreg_start + 20, len(lines))):
        if 'mcpOut.Scan()' in lines[j] and 'Read the tool list' not in lines[j]:
            # This is the actual scan call, not the comment
            pass
        if line.strip() == '// Read the tool list response':
            scan_line = j + 1  # line after comment
            break
    
    # Find lines: "// Read the tool list response", "s.mcpOut.Scan()", "toolsResp := s.mcpOut.Bytes()"
    for j in range(sendreg_start, min(sendreg_start + 20, len(lines))):
        if 'mcpOut.Scan()' in lines[j] and 'mcpOut.Scan' in lines[j]:
            scan_offset = j
            break
    
    if scan_offset:
        indent = lines[scan_offset][:len(lines[scan_offset]) - len(lines[scan_offset].lstrip())]
        indent_inner = indent + '\t'
        
        # Lines to replace: from initMsg write through flush to the scan/read
        # Find the initMsg write before the scan
        init_line = None
        for j in range(sendreg_start, scan_offset + 1):
            if 'mcpIn.Write(initMsg)' in lines[j]:
                init_line = j
                break
        
        # Find toolsResp line after scan
        toolsresp_line = scan_offset + 1  # usually next line
        if toolsresp_line < len(lines) and 'toolsResp' not in lines[toolsresp_line]:
            toolsresp_line = scan_offset + 2  # might have an extra line
        
        # Also find the flush line before scan
        flush_line = None
        for j in range(sendreg_start, scan_offset):
            if 'mcpIn.Flush()' in lines[j]:
                flush_line = j
        
        if init_line and flush_line:
            # Replace lines from init_line to toolsresp_line with the polling loop
            old_range_end = toolsresp_line + 1
            while old_range_end < len(lines) and lines[old_range_end].strip() == '':
                old_range_end += 1
            # But don't eat too far - stop before end of function
            
            new_lines = [
                indent + 'for i := 0; i < 15; i++ {\n',
                indent_inner + 'initMsg := json.RawMessage(`{"jsonrpc":"2.0","id":0,"method":"tools/list","params":{}}`)\n',
                indent_inner + 's.mcpIn.Write(initMsg)\n',
                indent_inner + "s.mcpIn.WriteByte('\\n')\n",
                indent_inner + 's.mcpIn.Flush()\n',
                indent + '\n',
                indent_inner + 'if s.mcpOut.Scan() {\n',
                indent_inner + '\tline := s.mcpOut.Bytes()\n',
                indent_inner + '\tif s.hasTools(line) || i >= 14 {\n',
                indent_inner + '\t\ttoolsResp = line\n',
                indent_inner + '\t\tbreak\n',
                indent_inner + '\t}\n',
                indent_inner + '\tlog.Printf("[mcp-relay] tools/list returned empty tools, retrying (%d/15)...", i+1)\n',
                indent_inner + '\ttime.Sleep(1 * time.Second)\n',
                indent_inner + '} else {\n',
                indent_inner + '\ttoolsResp = s.mcpOut.Bytes()\n',
                indent_inner + '\tbreak\n',
                indent_inner + '}\n',
                indent + '}\n',
            ]
            
            # Delete from init_line to toolsresp_line inclusive
            del lines[init_line:toolsresp_line + 1]
            # Insert new lines at init_line
            for n, new_line in enumerate(new_lines):
                lines.insert(init_line + n, new_line)
            
            print(f"Change 3: Added tool polling loop in sendRegister")
        else:
            print("WARN: Could not find initMsg/flush lines for change 3")
    else:
        print("WARN: Could not find mcpOut.Scan() in sendRegister")
else:
    print("WARN: Could not find sendRegister for change 3")

# 4. Add hasTools helper before init()
for i, line in enumerate(lines):
    if line.strip() == 'func init() {':
        # Insert helper before this
        has_tools_code = [
            '// hasTools checks if a tools/list response contains any tools.\n',
            'func (s *MCPSession) hasTools(data []byte) bool {\n',
            '\tvar resp struct {\n',
            '\t\tResult *struct {\n',
            '\t\t\tTools []any `json:"tools"`\n',
            '\t\t} `json:"result,omitempty"`\n',
            '\t\tTools []any `json:"tools,omitempty"`\n',
            '\t}\n',
            '\tif err := json.Unmarshal(data, &resp); err != nil {\n',
            '\t\treturn true // parse error, assume tools present\n',
            '\t}\n',
            '\tif resp.Result != nil {\n',
            '\t\treturn len(resp.Result.Tools) > 0\n',
            '\t}\n',
            '\treturn len(resp.Tools) > 0\n',
            '}\n',
            '\n',
        ]
        for n, new_line in enumerate(has_tools_code):
            lines.insert(i + n, new_line)
        print(f"Change 4: Added hasTools helper")
        break

with open(path, 'w') as f:
    f.writelines(lines)

print("\nAll changes applied successfully!")
