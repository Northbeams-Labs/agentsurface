package mcpservers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// Every run says what it did not look at. These are the blind spots of an
// on-disk MCP inventory, written plainly enough that a reader can decide
// whether the gap matters to them.

// uncoveredClients are clients known to support MCP that this table does not
// read. They are named rather than summarised so the list can be argued with.
var uncoveredClients = []string{
	"OpenAI Codex CLI (TOML config)",
	"Goose",
	"LM Studio",
	"Amazon Q Developer",
	"Kiro",
	"Warp",
	"Cherry Studio",
	"5ire",
	"BoltAI",
	"Visual Studio (the Windows IDE)",
}

func gapsFor(env model.Env, roots []string) []model.Gap {
	gaps := []model.Gap{
		{
			Area: "mcp servers: clients not read",
			Reason: "these clients support MCP and are not in this table, so servers configured only there are invisible to this run: " +
				strings.Join(uncoveredClients, ", "),
		},
		{
			Area:   "mcp servers: remote and cloud hosted",
			Reason: "a server connected inside a vendor account, such as a Claude.ai connector or a hosted MCP directory, is stored on the vendor's side and leaves nothing on this disk",
		},
		{
			Area:   "mcp servers: running processes",
			Reason: "this reads configuration files only. A server started by hand, by a wrapper script or by another program is running without appearing here",
		},
		{
			Area:   "mcp servers: Continue YAML config",
			Reason: continueYAMLReason(env, roots),
		},
		{
			Area:   "mcp servers: JetBrains AI Assistant",
			Reason: "JetBrains documents the MCP settings dialog but not where it writes on disk, so this reads mcp.xml under the IDE options directory and the project file under .junie. A configuration stored under another name is missed",
		},
		{
			Area:   "mcp servers: managed configuration",
			Reason: "MCP configuration deployed by an employer through device management is not read; only the files under this user account are",
		},
		{
			Area:   "mcp servers: Claude Desktop on Linux",
			Reason: "Anthropic ships Claude Desktop for macOS and Windows only. The Linux path checked here is the Electron default used by community builds and is unverified",
		},
		{
			Area:   "mcp servers: project directories",
			Reason: projectReason(roots),
		},
	}

	if env.HomeDir == "" {
		gaps = append(gaps, model.Gap{
			Area:   "mcp servers: user scope",
			Reason: "no home directory was given, so no user level client configuration was read at all",
		})
	}

	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" && !insideHome(env, x) {
		gaps = append(gaps, model.Gap{
			Area:   "mcp servers: XDG_CONFIG_HOME",
			Reason: "XDG_CONFIG_HOME points outside the home directory being scanned, so config under it was not read",
		})
	}
	return gaps
}

func projectReason(roots []string) string {
	if len(roots) == 0 {
		return fmt.Sprintf("no project directory was scanned, so a repository carrying its own MCP configuration is not counted. Project scope only covers %d levels below a directory you name", maxProjectDepth)
	}
	return fmt.Sprintf(
		"project scope covered %d levels below %s. A repository outside those directories can carry its own MCP configuration and is not counted. Symlinks pointing out of a root are not followed",
		maxProjectDepth, strings.Join(roots, ", "))
}

// continueYAMLReason names the YAML files that were seen but not parsed.
// Continue's own format is YAML, and this tool has no YAML parser because it
// takes no third party dependencies, so the honest answer is a count and the
// directory to look in.
func continueYAMLReason(env model.Env, roots []string) string {
	var found []string
	globs := []string{
		filepath.Join(env.HomeDir, ".continue", "config.yaml"),
		filepath.Join(env.HomeDir, ".continue", "mcpServers", "*.yaml"),
		filepath.Join(env.HomeDir, ".continue", "mcpServers", "*.yml"),
	}
	for _, root := range roots {
		globs = append(globs,
			filepath.Join(root, ".continue", "mcpServers", "*.yaml"),
			filepath.Join(root, ".continue", "mcpServers", "*.yml"),
		)
	}
	for _, g := range globs {
		matches, err := filepath.Glob(g)
		if err != nil {
			continue
		}
		found = append(found, matches...)
	}

	base := "Continue also declares MCP servers in YAML, which this tool does not parse because it ships with no third party dependencies. JSON dropped into .continue/mcpServers is read"
	if len(found) == 0 {
		return base + ", and no YAML config was present"
	}
	return fmt.Sprintf("%s. %d YAML config file(s) were present and not parsed: %s", base, len(found), joinSome(found))
}
