package packages

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// A skill is a folder holding a SKILL.md whose front matter names it and says
// when to use it. Claude Code loads them from ~/.claude/skills for the user,
// from .claude/skills in a project, and from the skills folder of every
// installed plugin. The last of those is the interesting one: a skill can
// arrive on the machine as a side effect of installing a plugin, and it is
// instructions the model will follow.

// maxSkillDepth bounds the walk through a plugin's skills folder. Real layouts
// are two or three deep (skills/<category>/<name>/SKILL.md); anything deeper is
// not worth walking a dependency tree for.
const maxSkillDepth = 4

// skipDirs are never walked. They are large, they are not agent machinery, and
// a vendored copy of someone else's skill is not an installed skill.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	"vendor": true, "__pycache__": true, ".venv": true,
}

// walkSkillFiles returns every SKILL.md under root, in a stable order.
func walkSkillFiles(root string) []string {
	var out []string
	rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory stops that branch, not the walk.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			if strings.Count(filepath.Clean(path), string(filepath.Separator))-rootDepth > maxSkillDepth {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == "SKILL.md" {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// userSkills reads the skills the user keeps for themselves.
func (c *collect) userSkills() {
	dir := filepath.Join(c.env.HomeDir, ".claude", "skills")
	entries, err := readDirSorted(dir)
	if err != nil {
		c.fail(dir, err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "SKILL.md")
		f, ok := c.skillFinding(path, e.Name(), model.ScopeUser)
		if !ok {
			continue
		}
		f.Notes = append(f.Notes, "written or placed here by the user, not installed from a marketplace")
		c.add(f)
	}
}

// projectSkills reads the skills a repository carries. These matter because
// nobody installed them: they arrived with a clone.
func (c *collect) projectSkills() {
	for _, root := range c.env.Roots {
		dir := filepath.Join(root, ".claude", "skills")
		entries, err := readDirSorted(dir)
		if err != nil {
			c.fail(dir, err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(dir, e.Name(), "SKILL.md")
			f, ok := c.skillFinding(path, e.Name(), model.ScopeProject)
			if !ok {
				continue
			}
			f.Notes = append(f.Notes, "carried by this project directory, not installed by the user")
			c.add(f)
		}
	}
}

// pluginSkills reads the skills that came with an installed plugin.
func (c *collect) pluginSkills(dir, pluginName, market string, man pluginManifest, scope model.Scope) {
	paths := walkSkillFiles(filepath.Join(dir, "skills"))
	if root := filepath.Join(dir, "SKILL.md"); fileExists(root) {
		paths = append([]string{root}, paths...)
	}
	origin := "installed with plugin " + pluginName
	if market != "" {
		origin += " from marketplace " + market
	} else {
		origin += ", installed from a local path"
	}
	for _, path := range paths {
		f, ok := c.skillFinding(path, filepath.Base(filepath.Dir(path)), scope)
		if !ok {
			continue
		}
		f.Notes = append(f.Notes, origin)
		if f.Publisher == "" {
			f.Publisher = man.Author.Name
		}
		if f.Version == "" {
			f.Version = man.Version
		}
		c.add(f)
	}
}

// skillFinding reads one SKILL.md. A skill that cannot be read is reported and
// skipped; a directory with no SKILL.md is not a skill and is passed over in
// silence.
func (c *collect) skillFinding(path, fallbackName string, scope model.Scope) (model.Finding, bool) {
	b, err := readCapped(path)
	if err != nil {
		if os.IsNotExist(err) {
			return model.Finding{}, false
		}
		c.fail(abs(path), err)
		return model.Finding{}, false
	}

	front := parseFrontMatter(b)
	name := firstNonEmpty(front["name"], fallbackName)

	rs := newReachSet()
	tools := splitList(front["allowed-tools"])
	for _, t := range tools {
		rs.fromSkillTool(t)
	}

	// The body is hashed, not stored. A skill is instructions, and instructions
	// that change under the user is the thing drift detection is for.
	body := sha256.Sum256(b)

	f := model.Finding{
		Kind:    model.KindSkill,
		Name:    name,
		Client:  clientClaudeCode,
		Scope:   scope,
		Version: front["version"],
		Source:  abs(path),
		Reach:   rs.list(),
		Digest: digestOf("skill", name, front["version"], front["allowed-tools"],
			hex.EncodeToString(body[:])),
	}

	if len(tools) > 0 {
		f.Notes = append(f.Notes, "declares allowed tools: "+strings.Join(tools, ", "))
	} else {
		f.Notes = append(f.Notes, "declares no tool restriction, so it inherits the session's tools")
	}
	if scripts := countScripts(filepath.Dir(path)); scripts > 0 {
		f.Notes = append(f.Notes, "ships "+plural(scripts, "executable script file")+" alongside the instructions")
	}
	return f, true
}

// fromSkillTool maps a Claude Code tool name in a skill's allowed-tools list to
// the reach that tool has. Anything unrecognised is left out rather than
// guessed at.
func (rs reachSet) fromSkillTool(tool string) {
	// allowed-tools entries can be scoped, as in Bash(git status:*).
	base := tool
	if i := strings.IndexAny(base, "("); i >= 0 {
		base = base[:i]
	}
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "bash", "shell", "killshell", "bashoutput":
		rs.add(model.ReachShell)
	case "read", "write", "edit", "glob", "grep", "notebookedit":
		rs.add(model.ReachFilesystem)
	case "webfetch", "websearch":
		rs.add(model.ReachNetwork)
	}
}

// parseFrontMatter reads the flat scalar keys out of a SKILL.md front matter
// block. The front matter is YAML, but only flat keys carry the declaration
// this tool records, so it is read directly rather than adding a dependency.
func parseFrontMatter(b []byte) map[string]string {
	out := map[string]string{}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return out
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return out
	}
	for _, line := range strings.Split(text[4:4+end], "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "-") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

// splitList reads a comma separated or bracketed list from front matter.
func splitList(v string) []string {
	v = strings.TrimSpace(strings.Trim(strings.TrimSpace(v), "[]"))
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.Trim(strings.TrimSpace(p), `"'`))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// countScripts counts the runnable files sitting next to a skill. It is stated
// as an observation, not as a finding about the skill's behaviour: a skill that
// ships a script is a skill with a script, nothing more.
func countScripts(dir string) int {
	entries, err := readDirSorted(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".sh", ".bash", ".zsh", ".py", ".js", ".mjs", ".cjs", ".ts", ".rb", ".pl", ".ps1":
			n++
		}
	}
	return n
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
