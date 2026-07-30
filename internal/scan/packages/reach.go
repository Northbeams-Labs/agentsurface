package packages

import (
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// Reach is derived from the declaration and nothing else. A manifest that says
// its entry point is a shell has stated it can run commands; a manifest that
// says nothing gets model.ReachUnknown rather than a guess, because "we do not
// know" is a true statement and "safe" would not be.

// shellCommands run whatever string they are handed.
var shellCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "ksh": true,
	"dash": true, "cmd": true, "powershell": true, "pwsh": true,
}

// interpreterCommands execute a program file that ships inside the package.
// The package author chose the code, not the user, so this is recorded as the
// same reach as a shell.
var interpreterCommands = map[string]bool{
	"node": true, "nodejs": true, "deno": true, "bun": true,
	"python": true, "python3": true, "pythonw": true, "ruby": true,
	"perl": true, "php": true, "npx": true, "uv": true, "uvx": true,
	"pipx": true, "npm": true, "pnpm": true, "yarn": true,
}

// toolReach maps a declared tool name to the reach that name states outright.
// Matching is on the whole tool name and never on a fragment, because
// "execute_javascript" in a browser extension is not a shell and should not be
// filed as one.
var toolReach = map[string]model.Reach{
	"osascript":       model.ReachAppleScript,
	"applescript":     model.ReachAppleScript,
	"run_applescript": model.ReachAppleScript,

	"execute_command":       model.ReachShell,
	"run_command":           model.ReachShell,
	"run_shell_command":     model.ReachShell,
	"execute_bash":          model.ReachShell,
	"run_script":            model.ReachShell,
	"shell":                 model.ReachShell,
	"bash":                  model.ReachShell,
	"terminal":              model.ReachShell,
	"start_process":         model.ReachShell,
	"interact_with_process": model.ReachShell,
	"kill_process":          model.ReachShell,
	"list_processes":        model.ReachShell,

	"read_file":           model.ReachFilesystem,
	"read_text_file":      model.ReachFilesystem,
	"read_multiple_files": model.ReachFilesystem,
	"write_file":          model.ReachFilesystem,
	"edit_file":           model.ReachFilesystem,
	"edit_block":          model.ReachFilesystem,
	"create_directory":    model.ReachFilesystem,
	"list_directory":      model.ReachFilesystem,
	"directory_tree":      model.ReachFilesystem,
	"move_file":           model.ReachFilesystem,
	"search_files":        model.ReachFilesystem,
	"get_file_info":       model.ReachFilesystem,

	"fetch":        model.ReachNetwork,
	"fetch_url":    model.ReachNetwork,
	"http_request": model.ReachNetwork,
	"web_search":   model.ReachNetwork,
	"web_fetch":    model.ReachNetwork,

	"open_url":           model.ReachBrowserTabs,
	"list_tabs":          model.ReachBrowserTabs,
	"get_current_tab":    model.ReachBrowserTabs,
	"close_tab":          model.ReachBrowserTabs,
	"switch_to_tab":      model.ReachBrowserTabs,
	"reload_tab":         model.ReachBrowserTabs,
	"execute_javascript": model.ReachBrowserTabs,
	"get_page_content":   model.ReachBrowserTabs,

	"read_clipboard":  model.ReachClipboard,
	"write_clipboard": model.ReachClipboard,
	"get_clipboard":   model.ReachClipboard,
	"set_clipboard":   model.ReachClipboard,

	"get_password":   model.ReachCredentials,
	"read_keychain":  model.ReachCredentials,
	"get_credential": model.ReachCredentials,
}

// reachSet collects reaches without duplicates and hands them back in a stable
// order, so that two runs over an unchanged machine produce identical output.
type reachSet map[model.Reach]bool

func newReachSet() reachSet { return reachSet{} }

func (rs reachSet) add(r model.Reach) { rs[r] = true }

// list returns the reaches found, or ReachUnknown if the declaration said
// nothing this scanner can read.
func (rs reachSet) list() []model.Reach {
	if len(rs) == 0 {
		return []model.Reach{model.ReachUnknown}
	}
	out := make([]model.Reach, 0, len(rs))
	for r := range rs {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// commandName strips the directory and any Windows extension off a declared
// command, so that "C:\\Program Files\\nodejs\\node.exe" and "node" match.
func commandName(cmd string) string {
	c := cmd
	if i := strings.LastIndexAny(c, `/\`); i >= 0 {
		c = c[i+1:]
	}
	c = strings.ToLower(strings.Trim(c, `"'`))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1", ".com"} {
		c = strings.TrimSuffix(c, ext)
	}
	return c
}

// fromCommand reads the declared entry point. osascript is AppleScript by
// definition; a shell or an interpreter can run whatever the package ships; a
// bundled binary is assumed to do neither, because the manifest did not say so.
func (rs reachSet) fromCommand(cmd string, args []string) {
	name := commandName(cmd)
	switch {
	case name == "osascript":
		rs.add(model.ReachAppleScript)
		rs.add(model.ReachShell)
	case shellCommands[name], interpreterCommands[name]:
		rs.add(model.ReachShell)
	}
	for _, a := range append([]string{cmd}, args...) {
		low := strings.ToLower(a)
		if strings.Contains(low, "osascript") {
			rs.add(model.ReachAppleScript)
		}
		if hasURL(low) {
			rs.add(model.ReachNetwork)
		}
	}
}

// fromTool files a declared tool under the reach its own name states.
func (rs reachSet) fromTool(name string) {
	if r, ok := toolReach[strings.ToLower(strings.TrimSpace(name))]; ok {
		rs.add(r)
	}
}

// fromUserConfigType reads the kind of value an extension asks the user for. A
// field of type "directory" or "file" is a declaration that the package wants
// filesystem access. The value the user gave is never read.
func (rs reachSet) fromUserConfigType(t string) {
	switch strings.ToLower(t) {
	case "directory", "file":
		rs.add(model.ReachFilesystem)
	}
}

// hasURL reports whether a declared string contains an http endpoint. Used for
// declared endpoints only; a homepage or a repository URL is metadata about the
// author and is not treated as reach.
func hasURL(s string) bool {
	return strings.Contains(s, "http://") || strings.Contains(s, "https://")
}

// agentBinaries is the conservative list of program names that mean an agent
// runs here. Matching is on a whole program name, which is why a wrapper script
// that starts an agent from the inside is missed; blindSpots says so out loud.
var agentBinaries = map[string]bool{
	"claude":       true,
	"claude-code":  true,
	"codex":        true,
	"gemini":       true,
	"cursor-agent": true,
	"aider":        true,
	"goose":        true,
	"opencode":     true,
	"crush":        true,
	"amp":          true,
	"copilot":      true,
	"ollama":       true,
}

// matchAgentBinary returns the agent binary named in an argument vector, or an
// empty string if none of them is one.
func matchAgentBinary(argv []string) string {
	for _, a := range argv {
		name := commandName(a)
		if agentBinaries[name] {
			return name
		}
	}
	return ""
}
