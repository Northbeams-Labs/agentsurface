package instructions

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// byName indexes a run's findings so a test can ask for one by its label.
func byName(findings []model.Finding) map[string]model.Finding {
	out := map[string]model.Finding{}
	for _, f := range findings {
		out[f.Name] = f
	}
	return out
}

func TestScanFindsEveryProjectForm(t *testing.T) {
	env := model.Env{OS: "darwin", Roots: []string{filepath.Join("testdata", "project")}}
	findings, gaps, errs := New().Scan(env)
	if len(errs) != 0 {
		t.Fatalf("unexpected scan errors: %v", errs)
	}

	want := map[string]string{
		"CLAUDE.md (project)":                               "Claude Code",
		".claude/CLAUDE.md (project)":                       "Claude Code",
		"CLAUDE.md (nested)":                                "Claude Code",
		"AGENTS.md (project)":                               "agents.md clients",
		".cursorrules (project)":                            "Cursor",
		".cursor/rules/style.mdc (project)":                 "Cursor",
		".github/copilot-instructions.md (project)":         "GitHub Copilot",
		".github/instructions/go.instructions.md (project)": "GitHub Copilot",
		".windsurfrules (project)":                          "Windsurf",
		".windsurf/rules/team.md (project)":                 "Windsurf",
		".clinerules/00-core.md (project)":                  "Cline",
		".rules (project)":                                  "Zed",
	}

	got := byName(findings)
	if len(findings) != len(want) {
		t.Errorf("got %d findings, want %d", len(findings), len(want))
		for _, f := range findings {
			t.Logf("  %s -> %s", f.Name, f.Source)
		}
	}
	for name, client := range want {
		f, ok := got[name]
		if !ok {
			t.Errorf("missing finding %q", name)
			continue
		}
		if f.Client != client {
			t.Errorf("%s: client = %q, want %q", name, f.Client, client)
		}
		if f.Kind != model.KindInstructionFile {
			t.Errorf("%s: kind = %q", name, f.Kind)
		}
		if f.Scope != model.ScopeProject {
			t.Errorf("%s: scope = %q, want project", name, f.Scope)
		}
		if len(f.Digest) != 64 {
			t.Errorf("%s: digest = %q", name, f.Digest)
		}
		if !filepath.IsAbs(f.Source) {
			t.Errorf("%s: source %q is not absolute", name, f.Source)
		}
		if len(f.Notes) == 0 {
			t.Errorf("%s: no notes", name)
		}
	}

	for _, f := range findings {
		if strings.Contains(f.Source, "node_modules") {
			t.Errorf("reported a file inside node_modules: %s", f.Source)
		}
	}
	if len(gaps) == 0 {
		t.Fatal("a scan must always say what it did not look at")
	}
	if !gapsMention(gaps, "node_modules") {
		t.Error("gaps do not mention the directories that were skipped")
	}
	if !gapsMention(gaps, "no home directory") {
		t.Error("gaps do not mention that user scope was not searched")
	}
}

func TestScanFindsUserScopeFiles(t *testing.T) {
	env := model.Env{OS: "darwin", HomeDir: filepath.Join("testdata", "home")}
	findings, _, errs := New().Scan(env)
	if len(errs) != 0 {
		t.Fatalf("unexpected scan errors: %v", errs)
	}

	want := map[string]string{
		"CLAUDE.md (user)":                "Claude Code",
		"project memory (user)":           "Claude Code",
		"AGENTS.md (user)":                "Codex CLI",
		"global_rules.md (user)":          "Windsurf",
		"00-global.md (user)":             "Cline",
		"security.instructions.md (user)": "GitHub Copilot",
	}
	got := byName(findings)
	if len(findings) != len(want) {
		t.Errorf("got %d user findings, want %d", len(findings), len(want))
		for _, f := range findings {
			t.Logf("  %s -> %s", f.Name, f.Source)
		}
	}
	for name, client := range want {
		f, ok := got[name]
		if !ok {
			t.Fatalf("missing user finding %q", name)
		}
		if f.Client != client {
			t.Errorf("%s: client = %q, want %q", name, f.Client, client)
		}
		if f.Scope != model.ScopeUser {
			t.Errorf("%s: scope = %q, want user", name, f.Scope)
		}
	}
}

func TestUserScopePathsAreOSSpecific(t *testing.T) {
	env := model.Env{OS: "windows", HomeDir: filepath.Join("testdata", "home")}
	findings, gaps, _ := New().Scan(env)
	if _, ok := byName(findings)["security.instructions.md (user)"]; ok {
		t.Error("the macOS VS Code path was searched on Windows")
	}
	if !gapsMention(gaps, "VS Code") {
		t.Error("a skipped client path must be recorded as a gap")
	}
}

func TestNotesAreObservations(t *testing.T) {
	env := model.Env{OS: "darwin", Roots: []string{filepath.Join("testdata", "project")}}
	findings, _, _ := New().Scan(env)
	notes := strings.Join(byName(findings)["CLAUDE.md (project)"].Notes, "\n")

	for _, want := range []string{
		"bytes, 14 lines",
		"line 3 imports another file: @./docs/style.md",
		"line 4 imports another file: @~/.claude/shared-conventions.md",
		"line 12 asks the agent to set aside earlier instructions",
		"line 13 asks the agent to keep something from the user",
		"line 14 asks the agent to act without user confirmation",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes are missing %q\ngot:\n%s", want, notes)
		}
	}
	for _, unwanted := range []string{"printed-not-imported", "example.com"} {
		if strings.Contains(notes, unwanted) {
			t.Errorf("notes contain %q, which is not an import\ngot:\n%s", unwanted, notes)
		}
	}
	// A quoted line is the whole quote: nothing longer than one line is ever
	// copied out of the file.
	for _, n := range byName(findings)["CLAUDE.md (project)"].Notes {
		if strings.Count(n, "\n") != 0 {
			t.Errorf("a note spans more than one line: %q", n)
		}
	}

	front := strings.Join(byName(findings)[".cursor/rules/style.mdc (project)"].Notes, "\n")
	if !strings.Contains(front, "front matter sets alwaysApply: true") {
		t.Errorf("front matter was not reported: %s", front)
	}

	hidden := strings.Join(byName(findings)[".rules (project)"].Notes, "\n")
	if !strings.Contains(hidden, "line 2 contains an invisible character (U+200B)") {
		t.Errorf("invisible character was not reported: %s", hidden)
	}
}

func TestLegacyAndDirectoryFormsAreBothReported(t *testing.T) {
	env := model.Env{OS: "darwin", Roots: []string{filepath.Join("testdata", "project")}}
	findings, _, _ := New().Scan(env)
	notes := strings.Join(byName(findings)[".cursorrules (project)"].Notes, "\n")
	if !strings.Contains(notes, "the newer .cursor/rules form is present") {
		t.Errorf("the competing form was not noted: %s", notes)
	}
}

func TestDigestIsTheFileHash(t *testing.T) {
	env := model.Env{OS: "darwin", Roots: []string{filepath.Join("testdata", "project")}}
	findings, _, _ := New().Scan(env)
	f := byName(findings)["AGENTS.md (project)"]

	b, err := os.ReadFile(f.Source)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if want := hex.EncodeToString(sum[:]); f.Digest != want {
		t.Errorf("digest = %s, want %s", f.Digest, want)
	}
}

func TestBinaryFilesAreSkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("intro\x00\x01binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	findings, gaps, errs := New().Scan(model.Env{OS: "darwin", Roots: []string{root}})
	if len(findings) != 0 {
		t.Fatalf("a binary file was reported as an instruction file: %+v", findings)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if !gapsMention(gaps, "null bytes") {
		t.Error("skipping a binary file must be recorded as a gap")
	}
}

func TestSymlinksAreNotFollowed(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "CLAUDE.md")
	if err := os.WriteFile(target, []byte("outside the root"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	findings, _, _ := New().Scan(model.Env{OS: "darwin", Roots: []string{root}})
	if len(findings) != 0 {
		t.Fatalf("a symlink was followed out of the root: %+v", findings)
	}
}

func TestGeneratedDirectoriesAreNotWalked(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".git", "vendor", filepath.Join("node_modules", "pkg"), "dist"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "AGENTS.md"), []byte("hidden"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	findings, _, _ := New().Scan(model.Env{OS: "darwin", Roots: []string{root}})
	if len(findings) != 0 {
		t.Fatalf("walked into a generated directory: %+v", findings)
	}
}

func TestWalkStopsAtMaxDepth(t *testing.T) {
	root := t.TempDir()
	deep := root
	for i := 0; i < maxWalkDepth+2; i++ {
		deep = filepath.Join(deep, fmt.Sprintf("d%d", i))
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "AGENTS.md"), []byte("too deep"), 0o600); err != nil {
		t.Fatal(err)
	}
	findings, _, _ := New().Scan(model.Env{OS: "darwin", Roots: []string{root}})
	if len(findings) != 0 {
		t.Fatalf("walked past the depth limit: %+v", findings)
	}
}

func TestOversizeFileIsRecordedButNotOpened(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("line of instructions\n", 60000) // over 1 MiB
	path := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	findings, _, _ := New().Scan(model.Env{OS: "darwin", Roots: []string{root}})
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	sum := sha256.Sum256([]byte(big))
	if want := hex.EncodeToString(sum[:]); findings[0].Digest != want {
		t.Errorf("digest = %s, want the hash of the whole file %s", findings[0].Digest, want)
	}
	if !strings.Contains(strings.Join(findings[0].Notes, "\n"), "only the first 1048576 bytes were inspected") {
		t.Errorf("the run did not say it read only part of the file: %v", findings[0].Notes)
	}
}

func TestEmptyEnvironmentIsSafe(t *testing.T) {
	findings, gaps, errs := New().Scan(model.Env{})
	if len(findings) != 0 {
		t.Errorf("found %d items with nothing to scan", len(findings))
	}
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if !gapsMention(gaps, "no home directory") || !gapsMention(gaps, "no project root") {
		t.Errorf("an empty scan must say it looked nowhere: %v", gaps)
	}
}

func TestMissingRootIsReportedNotFatal(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here")
	findings, _, errs := New().Scan(model.Env{OS: "darwin", Roots: []string{missing, "   "}})
	if len(findings) != 0 {
		t.Errorf("found %d items under a missing root", len(findings))
	}
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Path, "not-here") {
		t.Errorf("error does not name the missing root: %+v", errs[0])
	}
}

func TestUnreadableFileIsReportedNotFatal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	root := t.TempDir()
	path := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	findings, _, errs := New().Scan(model.Env{OS: "darwin", Roots: []string{root}})
	if len(findings) != 0 {
		t.Errorf("reported a file it could not read: %+v", findings)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Path, "CLAUDE.md") {
		t.Errorf("got %v, want one error naming the file", errs)
	}
}

func TestOverlappingRootsReportEachFileOnce(t *testing.T) {
	root := filepath.Join("testdata", "project")
	env := model.Env{OS: "darwin", Roots: []string{root, filepath.Join(root, "sub"), root}}
	findings, _, _ := New().Scan(env)
	seen := map[string]int{}
	for _, f := range findings {
		seen[f.Source]++
	}
	for src, n := range seen {
		if n != 1 {
			t.Errorf("%s reported %d times", src, n)
		}
	}
}

func TestFindingsAreOrderedByPath(t *testing.T) {
	env := model.Env{OS: "darwin", HomeDir: filepath.Join("testdata", "home"), Roots: []string{filepath.Join("testdata", "project")}}
	findings, _, _ := New().Scan(env)
	for i := 1; i < len(findings); i++ {
		if findings[i-1].Source > findings[i].Source {
			t.Fatalf("findings are not sorted: %s before %s", findings[i-1].Source, findings[i].Source)
		}
	}
}

func gapsMention(gaps []model.Gap, substr string) bool {
	for _, g := range gaps {
		if strings.Contains(g.Reason, substr) {
			return true
		}
	}
	return false
}
