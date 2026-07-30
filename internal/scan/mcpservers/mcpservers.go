// Package mcpservers inventories the Model Context Protocol servers declared on
// this machine.
//
// An MCP server is a program an AI client is configured to launch, or a remote
// endpoint it is configured to call. Every client stores that configuration in
// its own file, in its own shape, in its own directory, and there is no
// registry. So this package carries a table of the clients it knows, one row
// per client per operating system, and reads exactly those files.
//
// Two rules shape the code. The first: a path that is wrong finds nothing and
// says nothing, which looks identical to a clean machine. Every path here comes
// from the client's own documentation or from the file observed on a real
// install, and anything that could not be verified is left out of the table and
// written into a gap instead. The second: an MCP server's env block is where
// API keys live, so values are dropped at parse time and only names survive.
package mcpservers

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

type scanner struct{}

// New returns the mcpservers scanner.
func New() model.Scanner { return scanner{} }

func (scanner) Name() string { return "mcpservers" }

// client is one place one client keeps its server declarations.
//
// path is a template expanded against the machine being scanned:
//
//	{home}    the user's home directory
//	{config}  $XDG_CONFIG_HOME when it sits under home, else {home}/.config
//	{appdata} %APPDATA% when it sits under home, else {home}/AppData/Roaming
//	{appsup}  {home}/Library/Application Support
//
// A path may contain one * segment, which is expanded as a glob. goos is the
// GOOS value the row applies to.
type client struct {
	name  string
	goos  string
	path  string
	scope model.Scope
	shape shapeFunc
}

// coreClients is the table. Sources for each path are noted beside the rows;
// where a client publishes no path, the row is absent and there is a gap.
var coreClients = []client{
	// Claude Desktop. macOS and Windows paths are from Anthropic's MCP quickstart.
	// There is no official Linux build; the Linux row is the Electron default
	// that the community builds use, and the gap says so.
	{"Claude Desktop", "darwin", "{appsup}/Claude/claude_desktop_config.json", model.ScopeUser, shapeMCPServers},
	{"Claude Desktop", "windows", "{appdata}/Claude/claude_desktop_config.json", model.ScopeUser, shapeMCPServers},
	{"Claude Desktop", "linux", "{config}/Claude/claude_desktop_config.json", model.ScopeUser, shapeMCPServers},

	// Claude Code keeps user scoped servers and a block per project directory
	// in one file. Confirmed against a live install.
	{"Claude Code", "", "{home}/.claude.json", model.ScopeUser, shapeClaudeCodeUser},

	// Cursor.
	{"Cursor", "", "{home}/.cursor/mcp.json", model.ScopeUser, shapeMCPServers},

	// Windsurf, from the Cascade MCP documentation.
	{"Windsurf", "", "{home}/.codeium/windsurf/mcp_config.json", model.ScopeUser, shapeMCPServers},

	// Zed. Zed uses ~/.config on macOS as well as on Linux.
	{"Zed", "darwin", "{home}/.config/zed/settings.json", model.ScopeUser, shapeContextServers},
	{"Zed", "linux", "{config}/zed/settings.json", model.ScopeUser, shapeContextServers},
	{"Zed", "windows", "{appdata}/Zed/settings.json", model.ScopeUser, shapeContextServers},

	// Cline's command line build, from the Cline MCP documentation. The editor
	// extension keeps its own file; see clineClients below.
	{"Cline", "", "{home}/.cline/mcp.json", model.ScopeUser, shapeMCPServers},

	// Continue. JSON dropped into this directory is read as is; Continue's own
	// YAML blocks are not parsed and are recorded as a gap.
	{"Continue", "", "{home}/.continue/mcpServers/*.json", model.ScopeUser, shapeMCPServers},
	{"Continue", "", "{home}/.continue/config.json", model.ScopeUser, shapeContinueLegacy},

	// Gemini CLI.
	{"Gemini CLI", "", "{home}/.gemini/settings.json", model.ScopeUser, shapeMCPServers},

	// JetBrains AI Assistant. JetBrains documents the settings dialog but not
	// where it writes, so this reads any mcp*.xml under the IDE options
	// directory and pulls the embedded JSON out of it. See the gap.
	{"JetBrains AI Assistant", "darwin", "{appsup}/JetBrains/*/options/mcp.xml", model.ScopeUser, shapeJetBrainsXML},
	{"JetBrains AI Assistant", "linux", "{config}/JetBrains/*/options/mcp.xml", model.ScopeUser, shapeJetBrainsXML},
	{"JetBrains AI Assistant", "windows", "{appdata}/JetBrains/*/options/mcp.xml", model.ScopeUser, shapeJetBrainsXML},
}

// vscodeEditions are the VS Code builds that carry GitHub Copilot agent mode.
var vscodeEditions = []struct{ display, dir string }{
	{"VS Code (GitHub Copilot)", "Code"},
	{"VS Code Insiders (GitHub Copilot)", "Code - Insiders"},
}

// clineHosts are the editors Cline installs into. The extension keeps its
// server list in the host editor's global storage, under its publisher id.
var clineHosts = []struct{ display, dir string }{
	{"Cline in VS Code", "Code"},
	{"Cline in VS Code Insiders", "Code - Insiders"},
	{"Cline in Cursor", "Cursor"},
	{"Cline in Windsurf", "Windsurf"},
}

// userDataDir is where the VS Code family keeps a user profile, per OS. %s is
// the editor's directory name.
var userDataDir = map[string]string{
	"darwin":  "{appsup}/%s/User",
	"linux":   "{config}/%s/User",
	"windows": "{appdata}/%s/User",
}

// clients is the whole table: the literal rows above plus the rows generated
// for the editors that differ only by directory name.
var clients = buildClients()

func buildClients() []client {
	out := append([]client(nil), coreClients...)
	for goos, tmpl := range userDataDir {
		for _, ed := range vscodeEditions {
			base := fmt.Sprintf(tmpl, ed.dir)
			// mcp.json in the default profile and in every named profile, plus
			// the older placement inside settings.json.
			out = append(out,
				client{ed.display, goos, base + "/mcp.json", model.ScopeUser, shapeServers},
				client{ed.display, goos, base + "/profiles/*/mcp.json", model.ScopeUser, shapeServers},
				client{ed.display, goos, base + "/settings.json", model.ScopeUser, shapeVSCodeSettings},
			)
		}
		for _, h := range clineHosts {
			base := fmt.Sprintf(tmpl, h.dir)
			out = append(out, client{
				h.display, goos,
				base + "/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json",
				model.ScopeUser, shapeMCPServers,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return out[i].path < out[j].path
	})
	return out
}

func (scanner) Scan(env model.Env) ([]model.Finding, []model.Gap, []model.ScanError) {
	var findings []model.Finding
	var errs []model.ScanError

	for _, c := range clients {
		// Without a home directory every template would expand to a relative
		// path and the scan would read whatever happens to be beside the
		// working directory. Skip user scope instead; the gap says so.
		if env.HomeDir == "" || (c.goos != "" && c.goos != env.OS) {
			continue
		}
		for _, path := range expandPath(env, c.path) {
			f, e := readConfig(c.name, path, c.scope, c.shape)
			findings = append(findings, f...)
			errs = append(errs, e...)
		}
	}

	projectFindings, projectErrs, roots := scanProjects(env)
	findings = append(findings, projectFindings...)
	errs = append(errs, projectErrs...)

	findings = dedupeFindings(findings)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Source != findings[j].Source {
			return findings[i].Source < findings[j].Source
		}
		return findings[i].Name < findings[j].Name
	})

	return findings, gapsFor(env, roots), errs
}

// readConfig reads one config file and turns it into findings. A file that is
// not there is not an error: most machines have most of these clients missing.
func readConfig(clientName, path string, scope model.Scope, shape shapeFunc) ([]model.Finding, []model.ScanError) {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		// A glob can match a directory whose name ends in .json. That is not a
		// config file and not a failure either.
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []model.ScanError{{Scanner: "mcpservers", Path: path, Err: readErrText(err)}}
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}

	servers, err := shape(data)
	if err != nil {
		return nil, []model.ScanError{{Scanner: "mcpservers", Path: path, Err: err.Error()}}
	}

	out := make([]model.Finding, 0, len(servers))
	for _, s := range servers {
		out = append(out, toFinding(clientName, path, scope, s))
	}
	return out, nil
}

func readErrText(err error) string {
	if errors.Is(err, fs.ErrPermission) {
		return "permission denied"
	}
	return err.Error()
}

func toFinding(clientName, path string, scope model.Scope, s server) model.Finding {
	if s.scope != "" {
		scope = s.scope
	}
	f := model.Finding{
		Kind:    model.KindMCPServer,
		Name:    s.name,
		Client:  clientName,
		Scope:   scope,
		Source:  absolute(path),
		Command: commandLine(s),
		Reach:   reachOf(s),
		Digest:  digestOf(s),
	}

	switch {
	case s.transport != "":
		f.Notes = append(f.Notes, "declared transport: "+s.transport)
	case s.url != "":
		f.Notes = append(f.Notes, "declared transport: remote endpoint")
	case s.command != "":
		f.Notes = append(f.Notes, "declared transport: stdio, launched as a local process")
	}
	if s.project != "" {
		f.Notes = append(f.Notes, "configured for the project directory "+s.project)
	}
	if len(s.envKeys) > 0 {
		f.Notes = append(f.Notes, "passes environment variables "+joinSome(s.envKeys)+"; values are not read")
	}
	if len(s.headerKeys) > 0 {
		f.Notes = append(f.Notes, "sends request headers "+joinSome(s.headerKeys)+"; values are not read")
	}
	if len(s.autoApprove) > 0 {
		f.Notes = append(f.Notes, "tools approved in advance: "+joinSome(s.autoApprove))
	}
	if s.disabled {
		f.Notes = append(f.Notes, "switched off in this client's configuration")
	}
	return f
}

// joinSome keeps a long list readable without hiding how long it is.
func joinSome(items []string) string {
	const max = 8
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:max], ", "), len(items)-max)
}

// expandPath turns one template into the concrete paths it matches.
func expandPath(env model.Env, tmpl string) []string {
	home := env.HomeDir
	repl := strings.NewReplacer(
		"{home}", home,
		"{config}", configDir(env),
		"{appdata}", appDataDir(env),
		"{appsup}", filepath.Join(home, "Library", "Application Support"),
	)
	p := filepath.FromSlash(repl.Replace(tmpl))
	if !strings.Contains(p, "*") {
		return []string{p}
	}
	matches, err := filepath.Glob(p)
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}

// configDir honours XDG_CONFIG_HOME, but only when it points inside the home
// directory being scanned. A fixture home must never pick up the developer's
// own environment, and a config directory outside home is recorded as a gap.
func configDir(env model.Env) string {
	if x := os.Getenv("XDG_CONFIG_HOME"); insideHome(env, x) {
		return x
	}
	return filepath.Join(env.HomeDir, ".config")
}

func appDataDir(env model.Env) string {
	if a := os.Getenv("APPDATA"); insideHome(env, a) {
		return a
	}
	return filepath.Join(env.HomeDir, "AppData", "Roaming")
}

func insideHome(env model.Env, dir string) bool {
	if dir == "" || env.HomeDir == "" || !filepath.IsAbs(dir) {
		return false
	}
	rel, err := filepath.Rel(env.HomeDir, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func absolute(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// dedupeFindings drops the same server read twice, which happens when one root
// sits inside another.
func dedupeFindings(in []model.Finding) []model.Finding {
	seen := map[string]bool{}
	out := in[:0]
	for _, f := range in {
		key := f.Source + "\x00" + f.Name + "\x00" + string(f.Scope)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}
