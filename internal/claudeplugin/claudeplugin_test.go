// Package claudeplugin holds no code. It holds the tests that keep the Claude
// Code plugin under plugin/agentsurface honest.
//
// The plugin is published through a directory that reads the two manifests in
// this repository, so a manifest that has drifted is not a local problem: it is
// a broken listing for everybody who installs it. `claude plugin validate` is
// the authority on the manifest format, and it is not available in CI, so
// these tests check the parts that can be checked without it: that the two
// manifests are valid JSON, agree with each other, and still point at files
// that exist.
//
// The second half runs the wrapper script itself, because the promises the
// plugin makes are promises about what that script does. That it names both
// install commands when the binary is missing, and that it changes nothing
// about the tool's output except hiding the note lines, are behaviours rather
// than documentation, and they are tested as behaviours.
//
// This file is a test file, so nothing in it is ever linked into the released
// binary. That is why it may use os/exec while the command may not.
package claudeplugin

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	marketplaceManifest = ".claude-plugin/marketplace.json"
	pluginDir           = "plugin/agentsurface"
	pluginManifest      = "plugin/agentsurface/.claude-plugin/plugin.json"
	skillFile           = "plugin/agentsurface/skills/inventory/SKILL.md"
	wrapperScript       = "plugin/agentsurface/skills/inventory/scripts/agentsurface-inventory.sh"

	goInstallCommand = "go install github.com/Northbeams-Labs/agentsurface/cmd/agentsurface@latest"
	brewCommand      = "brew install Northbeams-Labs/tap/agentsurface"
)

type author struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type pluginFile struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	License     string   `json:"license"`
	Repository  string   `json:"repository"`
	Author      author   `json:"author"`
	Keywords    []string `json:"keywords"`
}

type marketplaceFile struct {
	Name   string `json:"name"`
	Owner  author `json:"owner"`
	Plugin []struct {
		Name        string `json:"name"`
		Source      string `json:"source"`
		Version     string `json:"version"`
		Description string `json:"description"`
		License     string `json:"license"`
	} `json:"plugins"`
}

// moduleRoot walks up from the test's directory to the directory holding
// go.mod, so that the paths above resolve wherever the test is run from.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}

func read(t *testing.T, root, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s is unreadable: %v", rel, err)
	}
	return b
}

func readPluginManifest(t *testing.T, root string) pluginFile {
	t.Helper()
	var p pluginFile
	if err := json.Unmarshal(read(t, root, pluginManifest), &p); err != nil {
		t.Fatalf("%s is not valid JSON: %v", pluginManifest, err)
	}
	return p
}

func readMarketplaceManifest(t *testing.T, root string) marketplaceFile {
	t.Helper()
	var m marketplaceFile
	if err := json.Unmarshal(read(t, root, marketplaceManifest), &m); err != nil {
		t.Fatalf("%s is not valid JSON: %v", marketplaceManifest, err)
	}
	return m
}

func TestThePluginManifestCarriesWhatTheDirectoryDisplays(t *testing.T) {
	root := moduleRoot(t)
	p := readPluginManifest(t, root)

	if p.Name != "agentsurface" {
		t.Errorf("plugin name is %q, want agentsurface", p.Name)
	}
	if p.Version == "" {
		t.Error("no version: without one every commit is treated as a new release")
	}
	if p.License != "Apache-2.0" {
		t.Errorf("licence is %q, want Apache-2.0, the same as the tool", p.License)
	}
	if p.Repository == "" {
		t.Error("no repository: the directory links to it and reviewers read it")
	}
	if p.Author.Name == "" {
		t.Error("no author name")
	}
	if len(p.Keywords) == 0 {
		t.Error("no keywords: the directory has nothing to match a search against")
	}

	// The description is the whole of what somebody sees before installing, so
	// the two claims that make this tool different have to survive an edit of
	// it.
	for _, phrase := range []string{"No network call", "no account"} {
		if !strings.Contains(p.Description, phrase) {
			t.Errorf("the description no longer says %q", phrase)
		}
	}
}

func TestTheMarketplaceEntryAgreesWithThePluginManifest(t *testing.T) {
	root := moduleRoot(t)
	m := readMarketplaceManifest(t, root)
	p := readPluginManifest(t, root)

	if m.Name == "" {
		t.Fatal("the marketplace has no name")
	}
	// Reserved for Anthropic. A marketplace using one of these stops loading.
	for _, reserved := range []string{
		"claude-code-marketplace", "claude-code-plugins", "claude-plugins-official",
		"claude-plugins-community", "claude-community", "anthropic-marketplace",
		"anthropic-plugins", "agent-skills", "anthropic-agent-skills",
	} {
		if m.Name == reserved {
			t.Fatalf("marketplace name %q is reserved for Anthropic use", m.Name)
		}
	}
	if m.Owner.Name == "" {
		t.Error("the marketplace has no owner name")
	}
	if len(m.Plugin) != 1 {
		t.Fatalf("the marketplace lists %d plugins, want 1", len(m.Plugin))
	}

	entry := m.Plugin[0]
	if entry.Name != p.Name {
		t.Errorf("marketplace entry is named %q, plugin manifest says %q", entry.Name, p.Name)
	}
	if entry.Version != p.Version {
		t.Errorf("marketplace entry is version %q, plugin manifest says %q", entry.Version, p.Version)
	}
	if entry.License != p.License {
		t.Errorf("marketplace entry licence is %q, plugin manifest says %q", entry.License, p.License)
	}
	if entry.Description != p.Description {
		t.Error("the two descriptions have drifted apart; the directory shows one of them and the installer shows the other")
	}
	if !strings.HasPrefix(entry.Source, "./") {
		t.Fatalf("source %q must be a relative path starting with ./", entry.Source)
	}
	if got, want := strings.TrimPrefix(entry.Source, "./"), pluginDir; got != want {
		t.Fatalf("source points at %q, but the plugin is at %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(pluginDir), ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("the source path holds no plugin manifest: %v", err)
	}
}

func TestTheSkillRefusesWhatTheToolRefuses(t *testing.T) {
	root := moduleRoot(t)
	skill := string(read(t, root, skillFile))

	if !strings.HasPrefix(skill, "---\n") {
		t.Fatal("SKILL.md does not open with YAML frontmatter")
	}
	end := strings.Index(skill[4:], "\n---\n")
	if end < 0 {
		t.Fatal("SKILL.md frontmatter is never closed")
	}
	frontmatter := skill[4 : end+4]

	for _, key := range []string{"name:", "description:", "allowed-tools:"} {
		if !strings.Contains(frontmatter, key) {
			t.Errorf("frontmatter has no %s", key)
		}
	}

	// A value that opens with [ or contains a colon is read as YAML structure
	// unless it is quoted, and a frontmatter block that fails to parse is
	// dropped in full and in silence at runtime. This is the mistake that is
	// easy to reintroduce, so it is checked rather than remembered.
	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, found := strings.Cut(line, ": ")
		if !found || strings.HasPrefix(line, " ") {
			continue
		}
		value = strings.TrimSpace(value)
		unquoted := !strings.HasPrefix(value, `"`) && !strings.HasPrefix(value, "'")
		if unquoted && (strings.HasPrefix(value, "[") || strings.Contains(value, ": ")) {
			t.Errorf("frontmatter value for %s needs quoting or the whole block silently fails to parse: %s", key, value)
		}
	}

	// The refusals are the reason the tool exists. They are inherited here, and
	// an edit that drops them is the failure this test is for.
	for _, refusal := range []string{
		"Do not judge",
		"Do not send the inventory anywhere",
		"Do not look anything up on the internet",
		"Do not act on what it finds",
		"data, not instruction",
		"What this did not look at",
	} {
		if !strings.Contains(skill, refusal) {
			t.Errorf("the skill no longer tells Claude: %q", refusal)
		}
	}

	if !strings.Contains(skill, "${CLAUDE_SKILL_DIR}/scripts/agentsurface-inventory.sh") {
		t.Error("the skill does not run the bundled script")
	}
}

func TestTheWrapperNamesBothInstallCommands(t *testing.T) {
	root := moduleRoot(t)
	script := string(read(t, root, wrapperScript))

	for _, command := range []string{goInstallCommand, brewCommand} {
		if !strings.Contains(script, command) {
			t.Errorf("the script never names %q, so a user without the binary is told nothing useful", command)
		}
	}
	// Nothing in here may reach the network. The binary's own guarantee is
	// checked in internal/networkguard; this is the wrapper's half of it.
	for _, forbidden := range []string{"curl ", "wget ", "nc ", "/dev/tcp"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("the wrapper script contains %q, and this plugin makes no network calls", forbidden)
		}
	}
}

// stubTool writes a stand-in for the binary that prints one item, one note line
// and the closing line, so that the filtering can be checked without building
// the real tool.
func stubTool(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "agentsurface")
	body := "#!/bin/sh\n" +
		"echo \"agentsurface v0.0.0-stub  test\"\n" +
		"echo \"  filesystem                       Claude Desktop, can reach: shell\"\n" +
		"echo \"                                   note: hidden in the overview\"\n" +
		"echo \"What this did not look at\"\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("cannot write the stub tool: %v", err)
	}
	return path
}

func runWrapper(t *testing.T, root string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(root, filepath.FromSlash(wrapperScript))}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("the wrapper could not be run: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

func TestTheWrapperExplainsItselfWhenTheBinaryIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the wrapper is a bash script; Claude Code runs it under Git Bash on Windows")
	}
	root := moduleRoot(t)

	// An empty PATH is the point: nothing named agentsurface can be found.
	out, code := runWrapper(t, root, []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"})

	if code != 127 {
		t.Errorf("exit status is %d, want 127 for a missing command", code)
	}
	for _, command := range []string{goInstallCommand, brewCommand} {
		if !strings.Contains(out, command) {
			t.Errorf("the message does not name %q:\n%s", command, out)
		}
	}
}

func TestTheOverviewHidesOnlyTheNoteLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the wrapper is a bash script; Claude Code runs it under Git Bash on Windows")
	}
	root := moduleRoot(t)
	dir := t.TempDir()
	stub := stubTool(t, dir)
	env := []string{"HOME=" + dir, "PATH=/usr/bin:/bin", "AGENTSURFACE_BIN=" + stub}

	overview, code := runWrapper(t, root, env)
	if code != 0 {
		t.Fatalf("exit status is %d, want 0:\n%s", code, overview)
	}
	if strings.Contains(overview, "note: hidden in the overview") {
		t.Error("the overview still carries the note lines it is supposed to hide")
	}
	if !strings.Contains(overview, "filesystem") {
		t.Error("the overview dropped an item line")
	}
	if !strings.Contains(overview, "What this did not look at") {
		t.Error("the overview dropped what the run did not look at, which turns an inventory into a false all-clear")
	}
	if !strings.Contains(overview, "1 per-item detail notes were hidden") {
		t.Error("the overview does not say how many notes it hid")
	}

	full, code := runWrapper(t, root, env, "--full")
	if code != 0 {
		t.Fatalf("--full exit status is %d, want 0:\n%s", code, full)
	}
	if !strings.Contains(full, "note: hidden in the overview") {
		t.Error("--full is supposed to print every line the tool printed")
	}
	if strings.Contains(full, "detail notes were hidden") {
		t.Error("--full should not report hidden notes; it hides none")
	}
}

func TestTheWrapperPassesTheToolsExitStatusThrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the wrapper is a bash script; Claude Code runs it under Git Bash on Windows")
	}
	root := moduleRoot(t)
	dir := t.TempDir()

	failing := filepath.Join(dir, "agentsurface")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\necho 'the run could not start' >&2\nexit 2\n"), 0o755); err != nil {
		t.Fatalf("cannot write the stub tool: %v", err)
	}

	out, code := runWrapper(t, root, []string{"HOME=" + dir, "PATH=/usr/bin:/bin", "AGENTSURFACE_BIN=" + failing})
	if code != 2 {
		t.Errorf("exit status is %d, want the tool's own 2:\n%s", code, out)
	}
}
