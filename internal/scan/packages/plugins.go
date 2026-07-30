package packages

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// A Claude Code plugin is a directory holding skills, agents, hooks and MCP
// server declarations, optionally described by .claude-plugin/plugin.json at
// its root. Installed plugins are recorded in
// ~/.claude/plugins/installed_plugins.json, which points at the unpacked copy
// under ~/.claude/plugins/cache/<marketplace>/<plugin>/<version>/. The
// marketplace each one came from is registered in known_marketplaces.json
// beside it, which is what separates "installed from a marketplace" from
// "written here".

type installedPlugins struct {
	Version int                          `json:"version"`
	Plugins map[string][]installedPlugin `json:"plugins"`
}

type installedPlugin struct {
	Scope        string `json:"scope"`
	InstallPath  string `json:"installPath"`
	Version      string `json:"version"`
	InstalledAt  string `json:"installedAt"`
	GitCommitSha string `json:"gitCommitSha"`
}

type knownMarketplace struct {
	Source struct {
		Source string `json:"source"`
		Repo   string `json:"repo"`
		URL    string `json:"url"`
		Path   string `json:"path"`
	} `json:"source"`
	InstallLocation string `json:"installLocation"`
	LastUpdated     string `json:"lastUpdated"`
}

// pluginManifest is .claude-plugin/plugin.json. Every field is optional; a
// plugin can be nothing but a directory with a skills folder in it.
type pluginManifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      person   `json:"author"`
	Homepage    string   `json:"homepage"`
	Skills      []string `json:"skills"`
	Commands    []string `json:"commands"`
	Agents      []string `json:"agents"`
	Hooks       any      `json:"hooks"`
	MCPServers  any      `json:"mcpServers"`
}

// marketplaceManifest is .claude-plugin/marketplace.json: a catalogue a
// repository offers to anyone who adds it.
type marketplaceManifest struct {
	Name    string `json:"name"`
	Owner   person `json:"owner"`
	Plugins []struct {
		Name string `json:"name"`
	} `json:"plugins"`
}

// claudeSettings is read for one key only. The rest of the file describes
// permissions and environment and is none of this scanner's business.
type claudeSettings struct {
	EnabledPlugins map[string]bool `json:"enabledPlugins"`
}

func (c *collect) claudeCodePlugins() {
	pluginsDir := filepath.Join(c.env.HomeDir, ".claude", "plugins")

	var installed installedPlugins
	installedPath := filepath.Join(pluginsDir, "installed_plugins.json")
	if err := readJSON(installedPath, &installed); err != nil {
		c.fail(installedPath, err)
		return
	}

	markets := map[string]knownMarketplace{}
	marketsPath := filepath.Join(pluginsDir, "known_marketplaces.json")
	if err := readJSON(marketsPath, &markets); err != nil {
		c.fail(marketsPath, err)
	}

	var settings claudeSettings
	settingsPath := filepath.Join(c.env.HomeDir, ".claude", "settings.json")
	if err := readJSON(settingsPath, &settings); err != nil {
		c.fail(settingsPath, err)
	}

	for _, key := range sortedKeys(installed.Plugins) {
		name, market := splitPluginKey(key)
		for _, inst := range installed.Plugins[key] {
			c.installedPlugin(key, name, market, inst, markets, settings)
		}
	}
}

func (c *collect) installedPlugin(key, name, market string, inst installedPlugin, markets map[string]knownMarketplace, settings claudeSettings) {
	dir := inst.InstallPath
	if dir == "" {
		c.failf(key, "installed plugin record has no install path")
		return
	}

	var man pluginManifest
	manifestPath := filepath.Join(dir, ".claude-plugin", "plugin.json")
	switch err := readJSON(manifestPath, &man); {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		// Documented and legal: a plugin whose components sit in the default
		// directories needs no manifest.
	default:
		c.fail(abs(manifestPath), err)
	}

	f := model.Finding{
		Kind:      model.KindPlugin,
		Name:      firstNonEmpty(man.Name, name),
		Client:    clientClaudeCode,
		Scope:     pluginScope(inst.Scope),
		Publisher: man.Author.Name,
		Version:   firstNonEmpty(inst.Version, man.Version),
		Source:    abs(dir),
	}

	if market != "" {
		if m, ok := markets[market]; ok {
			f.Notes = append(f.Notes, "installed from marketplace "+market+" ("+marketplaceOrigin(m)+")")
		} else {
			f.Notes = append(f.Notes, "installed from marketplace "+market+", which is not in the known marketplace list")
		}
	} else {
		f.Notes = append(f.Notes, "installed from a local path, not a marketplace")
	}
	if inst.GitCommitSha != "" {
		f.Notes = append(f.Notes, "pinned to commit "+inst.GitCommitSha)
	}
	if inst.InstalledAt != "" {
		f.Notes = append(f.Notes, "installed at: "+inst.InstalledAt)
	}
	if enabled, ok := settings.EnabledPlugins[key]; ok && !enabled {
		f.Notes = append(f.Notes, "present but switched off in settings.json")
	}

	rs := newReachSet()
	parts := c.pluginComponents(dir, rs)
	if len(parts) > 0 {
		f.Notes = append(f.Notes, "ships "+strings.Join(parts, ", "))
	}
	f.Reach = rs.list()
	f.Digest = digestOf("claude-code-plugin", man.Name, f.Version, inst.GitCommitSha,
		strings.Join(man.Skills, ","), strings.Join(parts, ","))

	c.add(f)

	// A plugin's skills are the part that actually reaches the model, so they
	// are inventoried in their own right rather than counted in a note.
	c.pluginSkills(dir, f.Name, market, man, f.Scope)
}

// pluginComponents reports what a plugin directory actually contains and adds
// the reach those components declare. Hooks and MCP server declarations are the
// two that run something.
func (c *collect) pluginComponents(dir string, rs reachSet) []string {
	var parts []string
	if n := countSkillDirs(dir); n > 0 {
		parts = append(parts, plural(n, "skill"))
	}
	for _, sub := range []string{"commands", "agents"} {
		if entries, err := readDirSorted(filepath.Join(dir, sub)); err == nil && len(entries) > 0 {
			parts = append(parts, plural(len(entries), strings.TrimSuffix(sub, "s")))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks", "hooks.json")); err == nil {
		// A hook is a command the client runs on an event, so a plugin that
		// ships hooks has declared it can run commands.
		parts = append(parts, "event hooks")
		rs.add(model.ReachShell)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); err == nil {
		parts = append(parts, "model context protocol servers")
	}
	return parts
}

// countSkillDirs counts SKILL.md files under a plugin, which may be nested a
// couple of folders deep. Vendored dependencies are skipped: node_modules is
// not agent machinery.
func countSkillDirs(dir string) int {
	n := len(walkSkillFiles(filepath.Join(dir, "skills")))
	if fileExists(filepath.Join(dir, "SKILL.md")) {
		n++
	}
	return n
}

func marketplaceOrigin(m knownMarketplace) string {
	switch {
	case m.Source.Repo != "":
		return m.Source.Source + ":" + m.Source.Repo
	case m.Source.URL != "":
		return m.Source.Source + ":" + m.Source.URL
	case m.Source.Source != "":
		return m.Source.Source
	default:
		return "origin not recorded"
	}
}

// splitPluginKey splits "plugin-name@marketplace-name". A key without an @ is a
// plugin that was installed from a path.
func splitPluginKey(key string) (name, market string) {
	if i := strings.LastIndex(key, "@"); i > 0 {
		return key[:i], key[i+1:]
	}
	return key, ""
}

func pluginScope(s string) model.Scope {
	if strings.EqualFold(s, string(model.ScopeProject)) {
		return model.ScopeProject
	}
	return model.ScopeUser
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// projectPlugins looks at the repositories being scanned. A checked-in
// .claude-plugin directory means the repository is itself agent machinery: it
// either is a plugin or offers a marketplace of them, and it arrived with the
// clone rather than by anyone installing it.
func (c *collect) projectPlugins() {
	for _, root := range c.env.Roots {
		manifestPath := filepath.Join(root, ".claude-plugin", "plugin.json")
		var man pluginManifest
		if err := readJSON(manifestPath, &man); err != nil {
			c.fail(abs(manifestPath), err)
		} else {
			rs := newReachSet()
			parts := c.pluginComponents(root, rs)
			f := model.Finding{
				Kind:      model.KindPlugin,
				Name:      firstNonEmpty(man.Name, filepath.Base(root)),
				Client:    clientClaudeCode,
				Scope:     model.ScopeProject,
				Publisher: man.Author.Name,
				Version:   man.Version,
				Source:    abs(manifestPath),
				Reach:     rs.list(),
				Digest:    digestOf("claude-code-project-plugin", man.Name, man.Version, strings.Join(man.Skills, ","), strings.Join(parts, ",")),
			}
			f.Notes = append(f.Notes, "defined in this project directory, not installed from a marketplace")
			if len(parts) > 0 {
				f.Notes = append(f.Notes, "ships "+strings.Join(parts, ", "))
			}
			c.add(f)
		}

		marketPath := filepath.Join(root, ".claude-plugin", "marketplace.json")
		var market marketplaceManifest
		if err := readJSON(marketPath, &market); err != nil {
			c.fail(abs(marketPath), err)
			continue
		}
		f := model.Finding{
			Kind:      model.KindPlugin,
			Name:      firstNonEmpty(market.Name, filepath.Base(root)),
			Client:    clientClaudeCode,
			Scope:     model.ScopeProject,
			Publisher: market.Owner.Name,
			Source:    abs(marketPath),
			Reach:     []model.Reach{model.ReachUnknown},
			Digest:    digestOf("claude-code-marketplace", market.Name, fmt.Sprint(len(market.Plugins))),
		}
		f.Notes = append(f.Notes, fmt.Sprintf("a plugin marketplace defined in this project, offering %d plugins", len(market.Plugins)))
		c.add(f)
	}
}
