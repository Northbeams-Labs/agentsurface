package instructions

import (
	"path"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// Every path in this file was checked against the client's own documentation
// before it was added. Paths that could not be confirmed are deliberately left
// out and named in a Gap instead: a wrong path here is a silent blind spot,
// while a missing one is at least printed on every run.

// userSpec is an instruction file the client reads for the whole user account,
// given as a path relative to the home directory. A spec whose path contains a
// star is expanded with filepath.Glob.
type userSpec struct {
	client string
	// rel is slash separated and relative to the home directory.
	rel string
	// name is the label for the finding. Empty means use the file's own name.
	name string
	// onlyOS restricts the spec to one operating system. Empty means any.
	onlyOS string
}

// userSpecs are the user-scope instruction files this build knows about.
var userSpecs = []userSpec{
	// Claude Code reads ~/.claude/CLAUDE.md as user memory for every project.
	{client: "Claude Code", rel: ".claude/CLAUDE.md", name: "CLAUDE.md"},
	// Claude Code keeps a per-project memory file under ~/.claude/projects and
	// loads it into the session automatically. Confirmed by observation of a
	// live Claude Code session on this platform, not from published docs.
	{client: "Claude Code", rel: ".claude/projects/*/memory/MEMORY.md", name: "project memory"},
	// Codex CLI reads AGENTS.md from its home ($CODEX_HOME, default ~/.codex)
	// before the repository's own AGENTS.md.
	{client: "Codex CLI", rel: ".codex/AGENTS.md", name: "AGENTS.md"},
	// Windsurf global rules, written by the app's own rules editor.
	{client: "Windsurf", rel: ".codeium/windsurf/memories/global_rules.md", name: "global_rules.md"},
	// Cline global rules directory, applied to every workspace.
	{client: "Cline", rel: "Documents/Cline/Rules/*.md"},
	// VS Code user-profile instruction files, read by GitHub Copilot in
	// addition to the ones in the repository.
	{client: "GitHub Copilot", rel: "Library/Application Support/Code/User/prompts/*.instructions.md", onlyOS: "darwin"},
	{client: "GitHub Copilot", rel: "Library/Application Support/Code - Insiders/User/prompts/*.instructions.md", onlyOS: "darwin"},
	{client: "GitHub Copilot", rel: ".config/Code/User/prompts/*.instructions.md", onlyOS: "linux"},
	{client: "GitHub Copilot", rel: ".config/Code - Insiders/User/prompts/*.instructions.md", onlyOS: "linux"},
}

// projectMatch classifies a file found under a project root. rel is the path
// relative to that root, slash separated. form is the canonical tail of the
// path for the matched shape, so the caller can tell a file at the root of the
// project from the same shape nested deeper down.
func projectMatch(rel string) (client, form string, ok bool) {
	base := path.Base(rel)
	dir := path.Dir(rel)

	switch base {
	// Claude Code reads CLAUDE.md from the project root, from .claude/, and
	// from any subdirectory it is working in. CLAUDE.local.md is the older
	// personal, gitignored form and is still read.
	case "CLAUDE.md", "CLAUDE.local.md":
		if path.Base(dir) == ".claude" {
			return "Claude Code", ".claude/" + base, true
		}
		return "Claude Code", base, true
	// AGENTS.md is the shared open format; Codex, Cursor, Copilot coding agent,
	// Zed and others all read it. The file nearest the work takes precedence.
	case "AGENTS.md":
		return "agents.md clients", base, true
	// Cursor's original single-file form, superseded by .cursor/rules but still
	// honoured.
	case ".cursorrules":
		return "Cursor", base, true
	// Windsurf's original single-file form, superseded by .windsurf/rules.
	case ".windsurfrules":
		return "Windsurf", base, true
	// Cline reads .clinerules either as a single file or as a directory.
	case ".clinerules":
		return "Cline", base, true
	// Zed's project rules file.
	case ".rules":
		return "Zed", base, true
	case "copilot-instructions.md":
		if path.Base(dir) == ".github" {
			return "GitHub Copilot", ".github/copilot-instructions.md", true
		}
	}

	ext := path.Ext(base)
	switch {
	// Cursor's directory form. Rules live in .cursor/rules as .mdc files and
	// nested .cursor/rules directories apply to their own subtree.
	case underDir(dir, ".cursor/rules") && (ext == ".mdc" || ext == ".md"):
		return "Cursor", tailFrom(rel, ".cursor/rules"), true
	// Windsurf's directory form.
	case underDir(dir, ".windsurf/rules") && ext == ".md":
		return "Windsurf", tailFrom(rel, ".windsurf/rules"), true
	// Cline's directory form, including its workflows subdirectory.
	case underDir(dir, ".clinerules") && ext == ".md":
		return "Cline", tailFrom(rel, ".clinerules"), true
	// Copilot's path-specific instructions, selected by their applyTo header.
	case underDir(dir, ".github/instructions") && strings.HasSuffix(base, ".instructions.md"):
		return "GitHub Copilot", tailFrom(rel, ".github/instructions"), true
	}

	return "", "", false
}

// underDir reports whether dir contains sub as a run of whole path segments.
func underDir(dir, sub string) bool {
	if dir == "" || dir == "." {
		return false
	}
	return strings.Contains("/"+dir+"/", "/"+sub+"/")
}

// tailFrom returns rel from the first segment of anchor onwards.
func tailFrom(rel, anchor string) string {
	first := strings.SplitN(anchor, "/", 2)[0]
	i := strings.Index("/"+rel, "/"+first+"/")
	if i < 0 {
		return rel
	}
	return rel[i:]
}

// dirsNotWalked are directories the walk refuses to descend into. They are
// third-party or generated trees: walking them is slow and what is inside them
// is not the user's own configuration. Every run says out loud that they were
// skipped.
var dirsNotWalked = map[string]bool{
	"node_modules": true, ".git": true, ".hg": true, ".svn": true,
	"vendor": true, "dist": true, "build": true, "out": true, "target": true,
	".venv": true, "venv": true, "site-packages": true, "__pycache__": true,
	".next": true, ".nuxt": true, ".cache": true, ".terraform": true,
	"Pods": true, ".gradle": true, ".tox": true, ".mypy_cache": true,
	".pytest_cache": true, "bower_components": true, ".yarn": true,
	".pnpm-store": true, "coverage": true, ".idea": true, "Carthage": true,
}

// staticGaps are the blind spots this scanner has on every run, whatever it
// finds. They are printed even when the scan goes perfectly.
func staticGaps(env model.Env) []model.Gap {
	gaps := []model.Gap{
		{Area: "instructions", Reason: "the walk does not descend into node_modules, .git, vendor, dist, build and similar generated directories, so instruction files inside them were not read"},
		{Area: "instructions", Reason: "symbolic links are never followed, so instruction files reachable only through a link were not read"},
		{Area: "instructions", Reason: "only the home directory and the scanned project roots were read, so system-wide and enterprise-managed instruction files elsewhere on disk were not"},
		{Area: "instructions", Reason: "Cursor and Zed keep their user-level rules in application state rather than in a file on disk, so those rules were not read"},
		{Area: "instructions", Reason: "files are hashed in full but only their first 1 MiB is inspected for imports and wording, and files containing null bytes are skipped as binary"},
		{Area: "instructions", Reason: "instruction files belonging to clients this build does not know, for example Gemini CLI, Continue, Roo Code, Junie and Aider, were not searched"},
		{Area: "instructions", Reason: "Claude Code subagents, skills and slash commands also carry instructions and are not counted here"},
		{Area: "instructions", Reason: "the wording check reports the lines it matched and nothing else; it cannot see an instruction phrased in a way it does not match, and a match is an observation rather than a judgement"},
	}
	if env.OS != "darwin" && env.OS != "linux" {
		gaps = append(gaps, model.Gap{Area: "instructions", Reason: "user-profile instruction files for VS Code are only searched on macOS and Linux, so they were not read on this operating system"})
	}
	return gaps
}
