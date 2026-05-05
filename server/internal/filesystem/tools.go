// Package filesystem provides built-in filesystem MCP tools for Diane.
// These tools allow the cloud agent to read, write, search, and manage files
// on the local machine via the MCP relay. No external MCP server needed.
//
// Architecture:
//
//	Cloud Agent → Memory Platform Relay → WebSocket → diane mcp relay
//	    → handleMCPServeRequest() → filesystem.Call() → os.ReadFile/WriteFile/etc.
//
// Security (future):
//   - Path validation sandbox (allowed directories)
//   - Audit logging to Memory Platform
//   - Operation limits (file size, rate limits)
package filesystem

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// =============================================================================
// Tool Definitions
// =============================================================================

// allowedDirs is the list of directories the filesystem tools can access.
// When empty, all directories are allowed (current behavior).
// Future: configure via AgentToolConfig or config file.
var allowedDirs []string

// Init sets the allowed directories. Call once at startup.
// Pass nil or empty slice to allow all directories.
func Init(dirs []string) {
	allowedDirs = dirs
}

// IsAllowed checks if a path is within the allowed directories.
// Returns the cleaned absolute path and an error if not allowed.
// When allowedDirs is empty, all paths are allowed.
func IsAllowed(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	abs = filepath.Clean(abs)

	if len(allowedDirs) == 0 {
		return abs, nil
	}

	for _, dir := range allowedDirs {
		cleanDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		cleanDir = filepath.Clean(cleanDir)
		if abs == cleanDir || strings.HasPrefix(abs, cleanDir+string(filepath.Separator)) {
			return abs, nil
		}
	}

	return "", fmt.Errorf("access denied: %q is not within allowed directories", path)
}

// arg helpers
func getString(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

func getBool(args map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return defaultVal
}

func getInt(args map[string]interface{}, key string, defaultVal int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return defaultVal
}

func getStringArray(args map[string]interface{}, key string) []string {
	raw, ok := args[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// textContent wraps a result as MCP text content.
func textContent(data interface{}) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("%v", data),
			},
		},
	}
}

// jsonContent wraps a result as formatted JSON text content.
func jsonContent(data interface{}) (map[string]interface{}, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return textContent(fmt.Sprintf("%v", data)), nil
	}
	return textContent(string(b)), nil
}

// errResult creates an error tool result.
func errResult(msg string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type":  "text",
				"text":  msg,
				"level": "error",
			},
		},
		"isError": true,
	}
}

// =============================================================================
// Tool definitions for buildMCPToolList
// =============================================================================

// Tools returns the list of filesystem tool definitions.
func Tools() []map[string]interface{} {
	return []map[string]interface{}{
		// ── Read ──
		{
			"name":        "filesystem_read_file",
			"description": "Read the complete contents of a file. Returns the file content as text. Supports any text file. For binary files, returns file info instead. Path must be absolute or relative to workspace.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to the file to read",
					},
				},
				"required": []string{"path"},
			},
		},
		// ── Write ──
		{
			"name":        "filesystem_write_file",
			"description": "Create a new file or overwrite an existing file with new content. Creates parent directories if they don't exist.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path where to write the file",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		// ── List Directory ──
		{
			"name":        "filesystem_list_directory",
			"description": "List all files and directories in a path. Returns name, size, type, and modification time for each entry.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path of the directory to list",
					},
				},
				"required": []string{"path"},
			},
		},
		// ── Create Directory ──
		{
			"name":        "filesystem_create_directory",
			"description": "Create a new directory. Creates parent directories if they don't exist (like mkdir -p).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path of the directory to create",
					},
				},
				"required": []string{"path"},
			},
		},
		// ── Tree ──
		{
			"name":        "filesystem_tree",
			"description": "Get a hierarchical JSON representation of a directory structure. Shows files and subdirectories up to the specified depth.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path of the directory to traverse",
					},
					"depth": map[string]interface{}{
						"type":        "number",
						"description": "Maximum depth to traverse (default: 3, -1 = unlimited)",
						"default":     3,
					},
				},
				"required": []string{"path"},
			},
		},
		// ── Search Files ──
		{
			"name":        "filesystem_search_files",
			"description": "Recursively search for files matching a filename pattern (glob). Returns matching file paths with metadata.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Starting directory path for the search",
					},
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Glob pattern to match against file names (e.g., '*.go', '*.md', '**/*.txt')",
					},
					"max_results": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of results to return (default: 100)",
						"default":     100,
					},
				},
				"required": []string{"path", "pattern"},
			},
		},
		// ── Search Within Files (grep) ──
		{
			"name":        "filesystem_search_text",
			"description": "Search for text within file contents. Scans text files for matching substrings and returns file paths with line numbers. Binary files are automatically excluded.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Starting directory path for the search",
					},
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Text or regex pattern to search for within file contents",
					},
					"file_pattern": map[string]interface{}{
						"type":        "string",
						"description": "Optional glob pattern to filter files by name (e.g., '*.go' to only search .go files)",
					},
					"max_results": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of results to return (default: 50)",
						"default":     50,
					},
				},
				"required": []string{"path", "pattern"},
			},
		},
		// ── Get File Info ──
		{
			"name":        "filesystem_get_file_info",
			"description": "Get detailed metadata about a file or directory: size, modification time, permissions, and type.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to the file or directory",
					},
				},
				"required": []string{"path"},
			},
		},
		// ── Edit File (find+replace) ──
		{
			"name":        "filesystem_edit_file",
			"description": "Edit a file by finding and replacing text. Supports plain text matching (default) or regex. Can replace first occurrence only or all occurrences.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to the file to edit",
					},
					"find": map[string]interface{}{
						"type":        "string",
						"description": "Text or regex pattern to search for",
					},
					"replace": map[string]interface{}{
						"type":        "string",
						"description": "Text to replace matches with",
					},
					"regex": map[string]interface{}{
						"type":        "boolean",
						"description": "Treat find pattern as a regular expression (default: false)",
						"default":     false,
					},
					"all_occurrences": map[string]interface{}{
						"type":        "boolean",
						"description": "Replace all occurrences (default: true). If false, only the first match is replaced.",
						"default":     true,
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, show what would be changed without modifying the file (default: false)",
						"default":     false,
					},
				},
				"required": []string{"path", "find", "replace"},
			},
		},
		// ── Move/Rename ──
		{
			"name":        "filesystem_move_file",
			"description": "Move or rename a file or directory.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]interface{}{
						"type":        "string",
						"description": "Current absolute path of the file or directory",
					},
					"destination": map[string]interface{}{
						"type":        "string",
						"description": "New absolute path for the file or directory",
					},
				},
				"required": []string{"source", "destination"},
			},
		},
		// ── Copy ──
		{
			"name":        "filesystem_copy_file",
			"description": "Copy a file or directory to a new location.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path of the file or directory to copy",
					},
					"destination": map[string]interface{}{
						"type":        "string",
						"description": "Absolute destination path",
					},
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to recursively copy directories (default: false)",
						"default":     false,
					},
				},
				"required": []string{"source", "destination"},
			},
		},
		// ── Delete ──
		{
			"name":        "filesystem_delete_file",
			"description": "Delete a file or empty directory. Use recursive=true to delete non-empty directories.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to the file or directory to delete",
					},
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"description": "Recursively delete directories and their contents (default: false)",
						"default":     false,
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

// =============================================================================
// Tool Dispatch
// =============================================================================

// Call dispatches a tool call to the appropriate handler.
func Call(name string, args map[string]interface{}) (map[string]interface{}, error) {
	switch name {
	case "filesystem_read_file":
		return handleReadFile(args)
	case "filesystem_write_file":
		return handleWriteFile(args)
	case "filesystem_list_directory":
		return handleListDirectory(args)
	case "filesystem_create_directory":
		return handleCreateDirectory(args)
	case "filesystem_tree":
		return handleTree(args)
	case "filesystem_search_files":
		return handleSearchFiles(args)
	case "filesystem_search_text":
		return handleSearchText(args)
	case "filesystem_get_file_info":
		return handleGetFileInfo(args)
	case "filesystem_edit_file":
		return handleEditFile(args)
	case "filesystem_move_file":
		return handleMoveFile(args)
	case "filesystem_copy_file":
		return handleCopyFile(args)
	case "filesystem_delete_file":
		return handleDeleteFile(args)
	default:
		return nil, fmt.Errorf("unknown filesystem tool: %s", name)
	}
}

// =============================================================================
// Tool Handlers
// =============================================================================

func handleReadFile(args map[string]interface{}) (map[string]interface{}, error) {
	path := getString(args, "path")
	if path == "" {
		return errResult("path is required"), nil
	}

	validPath, err := IsAllowed(path)
	if err != nil {
		return errResult(err.Error()), nil
	}

	info, err := os.Stat(validPath)
	if err != nil {
		return errResult(fmt.Sprintf("cannot access %q: %v", path, err)), nil
	}

	if info.IsDir() {
		return errResult(fmt.Sprintf("%q is a directory, not a file. Use filesystem_list_directory instead.", path)), nil
	}

	// Check if binary
	if info.Size() > 0 && isBinary(validPath, info) {
		return jsonContent(map[string]interface{}{
			"path":      path,
			"size":      info.Size(),
			"is_binary": true,
			"message":   "Binary file cannot be displayed as text. Use filesystem_get_file_info for metadata.",
		})
	}

	data, err := os.ReadFile(validPath)
	if err != nil {
		return errResult(fmt.Sprintf("failed to read %q: %v", path, err)), nil
	}

	return textContent(string(data)), nil
}

func handleWriteFile(args map[string]interface{}) (map[string]interface{}, error) {
	path := getString(args, "path")
	content := getString(args, "content")
	if path == "" {
		return errResult("path is required"), nil
	}

	validPath, err := IsAllowed(path)
	if err != nil {
		return errResult(err.Error()), nil
	}

	// Create parent directories
	parent := filepath.Dir(validPath)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return errResult(fmt.Sprintf("failed to create parent directories: %v", err)), nil
	}

	if err := os.WriteFile(validPath, []byte(content), 0644); err != nil {
		return errResult(fmt.Sprintf("failed to write %q: %v", path, err)), nil
	}

	return textContent(fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)), nil
}

func handleListDirectory(args map[string]interface{}) (map[string]interface{}, error) {
	path := getString(args, "path")
	if path == "" {
		return errResult("path is required"), nil
	}

	validPath, err := IsAllowed(path)
	if err != nil {
		return errResult(err.Error()), nil
	}

	entries, err := os.ReadDir(validPath)
	if err != nil {
		return errResult(fmt.Sprintf("failed to read directory %q: %v", path, err)), nil
	}

	type entryInfo struct {
		Name    string `json:"name"`
		IsDir   bool   `json:"is_dir"`
		Size    int64  `json:"size"`
		ModTime string `json:"modified_at"`
	}

	result := make([]entryInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		size := int64(0)
		modTime := ""
		if err == nil {
			size = info.Size()
			modTime = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
		result = append(result, entryInfo{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    size,
			ModTime: modTime,
		})
	}

	return jsonContent(result)
}

func handleCreateDirectory(args map[string]interface{}) (map[string]interface{}, error) {
	path := getString(args, "path")
	if path == "" {
		return errResult("path is required"), nil
	}

	validPath, err := IsAllowed(path)
	if err != nil {
		return errResult(err.Error()), nil
	}

	if err := os.MkdirAll(validPath, 0755); err != nil {
		return errResult(fmt.Sprintf("failed to create directory %q: %v", path, err)), nil
	}

	return textContent(fmt.Sprintf("Directory created: %s", path)), nil
}

func handleTree(args map[string]interface{}) (map[string]interface{}, error) {
	path := getString(args, "path")
	if path == "" {
		return errResult("path is required"), nil
	}

	validPath, err := IsAllowed(path)
	if err != nil {
		return errResult(err.Error()), nil
	}

	maxDepth := getInt(args, "depth", 3)

	type treeNode struct {
		Name     string      `json:"name"`
		IsDir    bool        `json:"is_dir"`
		Size     int64       `json:"size,omitempty"`
		Children []treeNode  `json:"children,omitempty"`
	}

	var walk func(dir string, depth int) ([]treeNode, error)
	walk = func(dir string, depth int) ([]treeNode, error) {
		if maxDepth >= 0 && depth > maxDepth {
			return nil, nil
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}

		nodes := make([]treeNode, 0, len(entries))
		for _, e := range entries {
			node := treeNode{Name: e.Name(), IsDir: e.IsDir()}
			if e.IsDir() {
				children, err := walk(filepath.Join(dir, e.Name()), depth+1)
				if err == nil {
					node.Children = children
				}
			} else {
				info, err := e.Info()
				if err == nil {
					node.Size = info.Size()
				}
			}
			nodes = append(nodes, node)
		}
		return nodes, nil
	}

	rootInfo, _ := os.Stat(validPath)
	root := treeNode{
		Name:  filepath.Base(validPath),
		IsDir: rootInfo != nil && rootInfo.IsDir(),
	}
	if root.IsDir {
		children, err := walk(validPath, 1)
		if err != nil {
			return errResult(fmt.Sprintf("failed to traverse directory: %v", err)), nil
		}
		root.Children = children
	}

	return jsonContent(root)
}

func handleSearchFiles(args map[string]interface{}) (map[string]interface{}, error) {
	path := getString(args, "path")
	pattern := getString(args, "pattern")
	if path == "" || pattern == "" {
		return errResult("path and pattern are required"), nil
	}

	validPath, err := IsAllowed(path)
	if err != nil {
		return errResult(err.Error()), nil
	}

	maxResults := getInt(args, "max_results", 100)

	type match struct {
		Path     string `json:"path"`
		IsDir    bool   `json:"is_dir"`
		Size     int64  `json:"size,omitempty"`
		ModTime  string `json:"modified_at,omitempty"`
	}

	var results []match

	// Use filepath.Walk for recursive glob matching
	err = filepath.WalkDir(validPath, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if len(results) >= maxResults {
			return filepath.SkipAll
		}

		matched, err := filepath.Match(pattern, d.Name())
		if err != nil {
			return nil // invalid pattern for this path, skip
		}
		if !matched {
			// Try matching the full relative path
			rel, _ := filepath.Rel(validPath, p)
			if rel != "" {
				matched, _ = filepath.Match(pattern, rel)
			}
		}
		if !matched {
			return nil
		}

		info, err := d.Info()
		size := int64(0)
		modTime := ""
		if err == nil {
			size = info.Size()
			modTime = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
		results = append(results, match{
			Path:    p,
			IsDir:   d.IsDir(),
			Size:    size,
			ModTime: modTime,
		})
		return nil
	})

	if err != nil {
		return errResult(fmt.Sprintf("search failed: %v", err)), nil
	}

	return jsonContent(map[string]interface{}{
		"matches": results,
		"total":   len(results),
	})
}

func handleSearchText(args map[string]interface{}) (map[string]interface{}, error) {
	searchPath := getString(args, "path")
	pattern := getString(args, "pattern")
	if searchPath == "" || pattern == "" {
		return errResult("path and pattern are required"), nil
	}

	validPath, err := IsAllowed(searchPath)
	if err != nil {
		return errResult(err.Error()), nil
	}

	filePattern := getString(args, "file_pattern")
	maxResults := getInt(args, "max_results", 50)

	re, err := regexp.Compile(pattern)
	if err != nil {
		return errResult(fmt.Sprintf("invalid regex pattern: %v", err)), nil
	}

	type match struct {
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Content   string `json:"content"`
	}

	var results []match

	err = filepath.WalkDir(validPath, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || len(results) >= maxResults {
			return nil
		}
		if filePattern != "" {
			matched, _ := filepath.Match(filePattern, d.Name())
			if !matched {
				return nil
			}
		}

		// Skip binary files
		info, err := d.Info()
		if err != nil || info.Size() > 10*1024*1024 {
			return nil // skip large files
		}
		if isBinary(p, info) {
			return nil
		}

		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				results = append(results, match{
					Path:    p,
					Line:    i + 1,
					Content: strings.TrimSpace(line),
				})
				if len(results) >= maxResults {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	if err != nil {
		return errResult(fmt.Sprintf("search failed: %v", err)), nil
	}

	return jsonContent(map[string]interface{}{
		"matches": results,
		"total":   len(results),
	})
}

func handleGetFileInfo(args map[string]interface{}) (map[string]interface{}, error) {
	path := getString(args, "path")
	if path == "" {
		return errResult("path is required"), nil
	}

	validPath, err := IsAllowed(path)
	if err != nil {
		return errResult(err.Error()), nil
	}

	info, err := os.Stat(validPath)
	if err != nil {
		return errResult(fmt.Sprintf("cannot access %q: %v", path, err)), nil
	}

	return jsonContent(map[string]interface{}{
		"path":         path,
		"size":         info.Size(),
		"is_dir":       info.IsDir(),
		"mode":         info.Mode().String(),
		"modified_at":  info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		"is_symlink":   info.Mode()&os.ModeSymlink != 0,
	})
}

func handleEditFile(args map[string]interface{}) (map[string]interface{}, error) {
	path := getString(args, "path")
	find := getString(args, "find")
	replace := getString(args, "replace")
	if path == "" || find == "" {
		return errResult("path and find are required"), nil
	}

	validPath, err := IsAllowed(path)
	if err != nil {
		return errResult(err.Error()), nil
	}

	useRegex := getBool(args, "regex", false)
	allOccurrences := getBool(args, "all_occurrences", true)
	dryRun := getBool(args, "dry_run", false)

	// Check it's a file
	info, err := os.Stat(validPath)
	if err != nil {
		return errResult(fmt.Sprintf("cannot access %q: %v", path, err)), nil
	}
	if info.IsDir() {
		return errResult(fmt.Sprintf("%q is a directory, cannot edit", path)), nil
	}

	data, err := os.ReadFile(validPath)
	if err != nil {
		return errResult(fmt.Sprintf("failed to read %q: %v", path, err)), nil
	}

	original := string(data)
	var modified string
	replacementCount := 0

	if useRegex {
		re, err := regexp.Compile(find)
		if err != nil {
			return errResult(fmt.Sprintf("invalid regex: %v", err)), nil
		}
		if allOccurrences {
			modified = re.ReplaceAllString(original, replace)
			replacementCount = len(re.FindAllString(original, -1))
		} else {
			loc := re.FindStringIndex(original)
			if loc != nil {
				replacementCount = 1
				modified = original[:loc[0]] + replace + original[loc[1]:]
			} else {
				modified = original
			}
		}
	} else {
		if allOccurrences {
			replacementCount = strings.Count(original, find)
			modified = strings.ReplaceAll(original, find, replace)
		} else {
			idx := strings.Index(original, find)
			if idx >= 0 {
				replacementCount = 1
				modified = original[:idx] + replace + original[idx+len(find):]
			} else {
				modified = original
			}
		}
	}

	if dryRun {
		return textContent(fmt.Sprintf("Dry run: %d replacement(s) would be made in %s", replacementCount, path)), nil
	}

	if replacementCount == 0 {
		return textContent(fmt.Sprintf("No matches found for %q in %s", find, path)), nil
	}

	if err := os.WriteFile(validPath, []byte(modified), info.Mode()); err != nil {
		return errResult(fmt.Sprintf("failed to write %q: %v", path, err)), nil
	}

	return textContent(fmt.Sprintf("Made %d replacement(s) in %s", replacementCount, path)), nil
}

func handleMoveFile(args map[string]interface{}) (map[string]interface{}, error) {
	source := getString(args, "source")
	dest := getString(args, "destination")
	if source == "" || dest == "" {
		return errResult("source and destination are required"), nil
	}

	validSource, err := IsAllowed(source)
	if err != nil {
		return errResult(err.Error()), nil
	}
	validDest, err := IsAllowed(dest)
	if err != nil {
		return errResult(err.Error()), nil
	}

	if err := os.Rename(validSource, validDest); err != nil {
		return errResult(fmt.Sprintf("failed to move %q to %q: %v", source, dest, err)), nil
	}

	return textContent(fmt.Sprintf("Moved %s → %s", source, dest)), nil
}

func handleCopyFile(args map[string]interface{}) (map[string]interface{}, error) {
	source := getString(args, "source")
	dest := getString(args, "destination")
	recursive := getBool(args, "recursive", false)
	if source == "" || dest == "" {
		return errResult("source and destination are required"), nil
	}

	validSource, err := IsAllowed(source)
	if err != nil {
		return errResult(err.Error()), nil
	}
	validDest, err := IsAllowed(dest)
	if err != nil {
		return errResult(err.Error()), nil
	}

	// Check source exists
	srcInfo, err := os.Stat(validSource)
	if err != nil {
		return errResult(fmt.Sprintf("cannot access %q: %v", source, err)), nil
	}

	if srcInfo.IsDir() && !recursive {
		return errResult(fmt.Sprintf("%q is a directory. Use recursive=true to copy directories.", source)), nil
	}

	if srcInfo.IsDir() {
		// Copy directory
		err = copyDir(validSource, validDest)
	} else {
		err = copyFile(validSource, validDest)
	}
	if err != nil {
		return errResult(fmt.Sprintf("failed to copy: %v", err)), nil
	}

	return textContent(fmt.Sprintf("Copied %s → %s", source, dest)), nil
}

func handleDeleteFile(args map[string]interface{}) (map[string]interface{}, error) {
	path := getString(args, "path")
	recursive := getBool(args, "recursive", false)
	if path == "" {
		return errResult("path is required"), nil
	}

	validPath, err := IsAllowed(path)
	if err != nil {
		return errResult(err.Error()), nil
	}

	info, err := os.Stat(validPath)
	if err != nil {
		return errResult(fmt.Sprintf("cannot access %q: %v", path, err)), nil
	}

	if info.IsDir() && !recursive {
		return errResult(fmt.Sprintf("%q is a directory. Use recursive=true to delete directories.", path)), nil
	}

	if info.IsDir() {
		if err := os.RemoveAll(validPath); err != nil {
			return errResult(fmt.Sprintf("failed to delete %q: %v", path, err)), nil
		}
	} else {
		if err := os.Remove(validPath); err != nil {
			return errResult(fmt.Sprintf("failed to delete %q: %v", path, err)), nil
		}
	}

	return textContent(fmt.Sprintf("Deleted: %s", path)), nil
}

// =============================================================================
// Utilities
// =============================================================================

// copyFile copies a single file.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return dstFile.Sync()
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

// isBinary checks if a file appears to be binary by checking its extension
// and the first bytes of content.
func isBinary(path string, info os.FileInfo) bool {
	// Known binary extensions
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".exe", ".dll", ".so", ".dylib", ".bin", ".o", ".a", ".lib",
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".ico", ".webp", ".avif",
		".mp3", ".mp4", ".avi", ".mov", ".mkv", ".wav", ".flac", ".ogg",
		".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar",
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".dmg", ".iso", ".ttf", ".otf", ".woff", ".woff2",
		".pyc", ".pyo", ".class", ".wasm":
		return true
	}

	// Check first bytes for null bytes (binary content)
	if info.Size() == 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}
	buf = buf[:n]
	for _, b := range buf {
		if b == 0 && b != '\n' && b != '\r' && b != '\t' {
			return true
		}
	}
	return false
}

// HashFile computes the SHA-256 hash of a file.
// Useful for future dedup and integrity checking.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
