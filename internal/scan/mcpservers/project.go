package mcpservers

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// Project scope is the half people forget. A repository can carry its own MCP
// configuration, so cloning it and opening it in an agent client is enough to
// have a server declared on your machine that you never installed.
//
// The walk is deliberately shallow and deliberately bounded. This tool is not a
// disk scanner: it looks at the roots it was given, a few levels down, and it
// does not follow a symlink out of the root it started in.

// projectFile is one agent config file a repository can carry, relative to a
// directory inside a root. rel may end in a * glob.
type projectFile struct {
	client string
	rel    string
	shape  shapeFunc
}

var projectFiles = []projectFile{
	{"Claude Code", ".mcp.json", shapeMCPServers},
	{"Cursor", ".cursor/mcp.json", shapeMCPServers},
	{"VS Code (GitHub Copilot)", ".vscode/mcp.json", shapeServers},
	{"Zed", ".zed/settings.json", shapeContextServers},
	{"Continue", ".continue/mcpServers/*.json", shapeMCPServers},
	{"Gemini CLI", ".gemini/settings.json", shapeMCPServers},
	// JetBrains Junie reads its project configuration from .junie. This is the
	// one project path in the table taken from JetBrains community usage rather
	// than a documented path, and the JetBrains gap says so.
	{"JetBrains Junie", ".junie/mcp/mcp.json", shapeMCPServers},
}

// maxProjectDepth is how far below a root the walk goes. Repositories keep
// agent configuration at or near the top; going deeper turns an inventory into
// a disk crawl.
const maxProjectDepth = 3

// skipDirs are directories that cannot hold a repository's own agent config but
// can hold thousands of files.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "out": true, "coverage": true, "tmp": true,
	"__pycache__": true, "venv": true, "Library": true, "Pods": true,
}

// scanProjects reads project scoped config under every root, and returns the
// roots it actually walked so the gap can name them.
func scanProjects(env model.Env) ([]model.Finding, []model.ScanError, []string) {
	roots := env.Roots
	if len(roots) == 0 {
		// model.Env documents empty roots as the current directory only.
		if cwd, err := os.Getwd(); err == nil {
			roots = []string{cwd}
		}
	}

	var findings []model.Finding
	var errs []model.ScanError
	var walked []string

	for _, root := range roots {
		root = absolute(root)
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			continue
		}
		walked = append(walked, root)
		for _, dir := range projectDirs(root) {
			for _, pf := range projectFiles {
				for _, path := range globUnder(dir, pf.rel) {
					if !insideRoot(root, path) {
						continue
					}
					f, e := readConfig(pf.client, path, model.ScopeProject, pf.shape)
					findings = append(findings, f...)
					errs = append(errs, e...)
				}
			}
		}
	}
	return findings, errs, walked
}

// projectDirs lists root and its descendants down to maxProjectDepth. Hidden
// directories are not descended into: the config files that live inside one are
// reached by name from the table instead.
func projectDirs(root string) []string {
	dirs := []string{root}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory is skipped rather than fatal; the file
			// level read reports permission problems where they matter.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		// WalkDir does not follow symlinks, so a symlinked directory arrives
		// here as a non-directory entry and never gets walked. This is what
		// keeps the walk inside the root.
		name := d.Name()
		if strings.HasPrefix(name, ".") || skipDirs[name] {
			return fs.SkipDir
		}
		if depthUnder(root, path) > maxProjectDepth {
			return fs.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	sort.Strings(dirs)
	return dirs
}

func depthUnder(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return maxProjectDepth + 1
	}
	return len(strings.Split(filepath.ToSlash(rel), "/"))
}

// globUnder resolves one relative candidate under dir, expanding a * segment.
// Only regular files are returned, and a symlink is resolved so the caller can
// check where it really points.
func globUnder(dir, rel string) []string {
	candidate := filepath.Join(dir, filepath.FromSlash(rel))
	var paths []string
	if strings.Contains(candidate, "*") {
		matches, err := filepath.Glob(candidate)
		if err != nil {
			return nil
		}
		paths = matches
	} else {
		paths = []string{candidate}
	}

	out := make([]string, 0, len(paths))
	for _, p := range paths {
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				continue
			}
			p = resolved
		}
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// insideRoot is the symlink guard: a config file that resolves outside the root
// it was found in is not read.
func insideRoot(root, path string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	rel, err := filepath.Rel(realRoot, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
