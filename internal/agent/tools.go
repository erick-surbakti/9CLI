package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/user/ai-cli/internal/llm"
)

const (
	maxReadBytes    = 51200
	maxCommandBytes = 30000
	maxSearchBytes  = 60000
	maxSearchLines  = 200
)

// Agent executes tools the model requests. baseDir is the workspace root.
type Agent struct {
	baseDir string
}

// New creates an agent rooted at the given directory.
func New(baseDir string) *Agent {
	return &Agent{baseDir: baseDir}
}

// ToolSpecs returns the schemas exposed to the model.
func (a *Agent) ToolSpecs() []llm.Tool {
	return []llm.Tool{
		{
			Type: "function",
			Function: llm.Function{
				Name:        "list_dir",
				Description: "List the files and directories at a given path. Use this to explore the workspace.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory to list. Defaults to the workspace root."}},"required":["path"]}`),
			},
		},
		{
			Type: "function",
			Function: llm.Function{
				Name:        "read_file",
				Description: "Read the contents of a text file on disk. Content is truncated if very large.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read."}},"required":["path"]}`),
			},
		},
		{
			Type: "function",
			Function: llm.Function{
				Name:        "write_file",
				Description: "Write or overwrite a text file on disk. Creating directories is automatic. The user must approve before this runs.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to write."},"content":{"type":"string","description":"Full new file content."}},"required":["path","content"]}`),
			},
		},
		{
			Type: "function",
			Function: llm.Function{
				Name:        "run_command",
				Description: "Run a shell command on the user's machine and return its output. The user must approve before this runs. Use for git, builds, tests, etc.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The shell command to run."}},"required":["command"]}`),
			},
		},
		{
			Type: "function",
			Function: llm.Function{
				Name:        "search_files",
				Description: "Search file contents in a directory (recursive) for a text query. Returns matching lines with file names.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Text to search for (case-insensitive)."},"path":{"type":"string","description":"Directory to search. Defaults to the workspace root."}},"required":["query"]}`),
			},
		},
	}
}

// Execute runs a single tool call and returns the result to feed back to the model.
func (a *Agent) Execute(call llm.ToolCall) (content string, isErr bool) {
	name := call.Function.Name
	var args map[string]string
	if len(call.Function.Arguments) > 0 {
		_ = json.Unmarshal(call.Function.Arguments, &args)
	}

	switch name {
	case "list_dir":
		content = a.listDir(args["path"])
	case "read_file":
		content, isErr = a.readFile(args["path"])
	case "write_file":
		content, isErr = a.writeFile(args["path"], args["content"])
	case "run_command":
		content, isErr = a.runCommand(args["command"])
	case "search_files":
		content, isErr = a.searchFiles(args["query"], args["path"])
	default:
		return fmt.Sprintf("unknown tool: %s", name), true
	}
	return content, isErr
}

func (a *Agent) resolve(path string) string {
	if path == "" || path == "." {
		return a.baseDir
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(a.baseDir, path)
}

func (a *Agent) listDir(path string) string {
	dir := a.resolve(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Contents of %s (%d entries):\n", dir, len(entries)))
	for _, e := range entries {
		if e.IsDir() {
			fmt.Fprintf(&b, "  %s/\n", e.Name())
		} else {
			info, err := e.Info()
			size := ""
			if err == nil {
				size = fmt.Sprintf(" (%d bytes)", info.Size())
			}
			fmt.Fprintf(&b, "  %s%s\n", e.Name(), size)
		}
	}
	return b.String()
}

func (a *Agent) readFile(path string) (string, bool) {
	full := a.resolve(path)
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	content := string(data)
	if len(content) > maxReadBytes {
		content = content[:maxReadBytes] + fmt.Sprintf("\n[... truncated, file is %d bytes total]\n", len(data))
	}
	return fmt.Sprintf("File %s (%d bytes):\n\n%s", full, len(data), content), false
}

func (a *Agent) writeFile(path, content string) (string, bool) {
	full := a.resolve(path)
	if content == "" {
		return "error: empty file content", true
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), full), false
}

func (a *Agent) runCommand(command string) (string, bool) {
	if strings.TrimSpace(command) == "" {
		return "error: empty command", true
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", command)
	} else {
		cmd = exec.Command("/bin/sh", "-c", command)
	}
	cmd.Dir = a.baseDir
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if len(text) > maxCommandBytes {
		text = text[:maxCommandBytes] + "\n[... output truncated]\n"
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return fmt.Sprintf("command exited with error: %v\n%s", err, text), true
	}
	return text, false
}

func (a *Agent) searchFiles(query, path string) (string, bool) {
	if strings.TrimSpace(query) == "" {
		return "error: empty query", true
	}
	q := strings.ToLower(query)
	root := a.resolve(path)

	var b strings.Builder
	var matches int
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(b.String()) >= maxSearchBytes || matches >= maxSearchLines {
			return filepath.SkipDir
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".exe" || ext == ".dll" || ext == ".bin" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if matches >= maxSearchLines {
				break
			}
			if strings.Contains(strings.ToLower(line), q) {
				rel, _ := filepath.Rel(a.baseDir, p)
				fmt.Fprintf(&b, "%s:%d: %s\n", rel, i+1, strings.TrimSpace(line))
				matches++
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Sprintf("error: %v", err), true
	}
	if matches == 0 {
		return fmt.Sprintf("No matches for %q in %s", query, root), false
	}
	return fmt.Sprintf("%d match(es) for %q in %s:\n%s", matches, query, root, b.String()), false
}
