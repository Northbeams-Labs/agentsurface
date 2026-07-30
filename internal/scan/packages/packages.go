// Package packages inventories the installed packages of agent machinery: the
// desktop extensions, plugins, skills, connectors and scheduled agent tasks
// that a client will load on its next start without the user installing
// anything again.
//
// Everything here is a local file read. Every path is derived from model.Env,
// so the whole scanner can be pointed at a fixture directory instead of the
// developer's own home.
//
// Two rules run through the whole package. Declared values only: a manifest
// says what an item can do, and that is what gets recorded, never the values a
// user later typed into it, because that is where the API keys live. And an
// absent thing is not an error: a machine that has never run Claude Desktop has
// no extensions directory, which is normal and silent.
package packages

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// maxFileBytes caps every file this scanner reads. Manifests are small; a
// multi-megabyte "manifest.json" is a reason to stop reading, not to allocate.
const maxFileBytes = 1 << 20

const (
	clientClaudeDesktop = "Claude Desktop"
	clientClaudeCode    = "Claude Code"
	clientGeminiCLI     = "Gemini CLI"
)

// scanner reads the package surfaces of the known agent clients.
//
// sysRoot is prefixed onto the machine-wide paths (/Library, /etc,
// C:\Windows). It is empty on a real run and set to a fixture directory in
// tests, which is the only way to exercise the system-scoped collectors
// without touching the machine the tests run on.
type scanner struct{ sysRoot string }

// New returns the packages scanner.
func New() model.Scanner { return scanner{} }

func (scanner) Name() string { return "packages" }

func (s scanner) Scan(env model.Env) ([]model.Finding, []model.Gap, []model.ScanError) {
	c := &collect{scanner: s, env: env}

	c.desktopExtensions()
	c.bundleFiles()
	c.geminiExtensions()
	c.claudeCodePlugins()
	c.projectPlugins()
	c.userSkills()
	c.projectSkills()
	c.connectors()
	c.scheduledTasks()
	c.blindSpots()

	return c.findings, c.gaps, c.errs
}

// collect accumulates one run. Each collector method appends and never aborts,
// because one unreadable directory must not cost the user the rest of the
// inventory.
type collect struct {
	scanner
	env      model.Env
	findings []model.Finding
	gaps     []model.Gap
	errs     []model.ScanError
}

func (c *collect) add(f model.Finding) { c.findings = append(c.findings, f) }

func (c *collect) gap(area, reason string) {
	c.gaps = append(c.gaps, model.Gap{Area: area, Reason: reason})
}

// fail records a failure worth telling the user about. A path that does not
// exist is the ordinary case on a machine that does not run that client, so it
// is not reported; permission denied and malformed content are.
func (c *collect) fail(path string, err error) {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return
	}
	c.errs = append(c.errs, model.ScanError{Scanner: "packages", Path: path, Err: err.Error()})
}

func (c *collect) failf(path, msg string) {
	c.errs = append(c.errs, model.ScanError{Scanner: "packages", Path: path, Err: msg})
}

// readJSON reads a small JSON file into v. A parse failure is returned as-is so
// the caller can report it against the path; malformed manifests are common
// enough that they must show up in the inventory rather than vanish.
func readJSON(path string, v any) error {
	b, err := readCapped(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, errors.New("expected a file, found a directory")
	}
	if st.Size() > maxFileBytes {
		return nil, errors.New("file is larger than this scanner reads")
	}
	return io.ReadAll(io.LimitReader(f, maxFileBytes))
}

// readDirSorted lists a directory in a stable order. os.ReadDir already sorts,
// but saying so here is what makes two runs comparable.
func readDirSorted(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// digestOf hashes the declared parts of an item that should not change on their
// own: its name, its version, its entry point, the tools and permissions it
// declares. Configured values never go in, because that is where secrets are,
// and a hash of a secret is still a fact about the secret.
func digestOf(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		io.WriteString(h, p)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// plural puts an s on a noun only when the count needs one, so a note reads
// "1 skill" rather than "1 skills".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// abs makes a source path absolute so a finding can be acted on from anywhere.
// If it cannot, the original path is still better than nothing.
func abs(path string) string {
	if a, err := filepath.Abs(path); err == nil {
		return a
	}
	return path
}

// claudeUserData is Electron's userData directory for Claude Desktop. The app
// itself builds its extension paths as join(app.getPath("userData"), ...), so
// the only per-OS part is where Electron puts userData.
func (c *collect) claudeUserData() string {
	switch c.env.OS {
	case "darwin":
		return filepath.Join(c.env.HomeDir, "Library", "Application Support", "Claude")
	case "windows":
		return filepath.Join(c.env.HomeDir, "AppData", "Roaming", "Claude")
	default:
		return filepath.Join(c.env.HomeDir, ".config", "Claude")
	}
}

// blindSpots states what this scanner did not look at. It is not an apology;
// it is the part of the output that lets someone tell "no findings" apart from
// "did not look".
func (c *collect) blindSpots() {
	c.gap("packages", "reach is read from what a manifest declares, not from what the code does; a package can declare nothing and still shell out at runtime")
	c.gap("packages", "no full-disk search: extension bundles are read from the client install directories and ~/Downloads only")

	if c.env.OS != "darwin" {
		c.gap("packages", "Claude Desktop ships for macOS and Windows only; on this OS the desktop extension directory is the Electron userData convention and may not exist")
	}
	c.gap("packages", "connectors added through a Claude account rather than a config file are held server side and are not on this machine")
	c.gap("packages", "clients without a documented on-disk package format (Cursor, Windsurf, VS Code agent modes) are not inventoried here")

	switch c.env.OS {
	case "darwin":
		c.gap("scheduled tasks", "launchd jobs are matched against a fixed list of agent binaries; a job that starts an agent indirectly, through a wrapper script or a shell one-liner, is not counted")
		c.gap("scheduled tasks", "binary property lists are not parsed; one whose raw bytes name an agent binary is reported as unreadable rather than passed over, and the user's own crontab is not read at all")
	case "windows":
		c.gap("scheduled tasks", "task XML under Windows\\System32\\Tasks and the Startup folder are read; registry Run keys are not")
	default:
		c.gap("scheduled tasks", "systemd user units and the readable crontab files are matched against a fixed list of agent binaries; a unit that starts an agent through a wrapper script is not counted")
	}
}

// trimSuffixFold removes an extension regardless of case.
func trimSuffixFold(s, suffix string) string {
	if len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix) {
		return s[:len(s)-len(suffix)]
	}
	return s
}
