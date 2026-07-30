package packages

import (
	"archive/zip"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// The fixtures hold values that must never leave the machine. If one of them
// turns up anywhere in the output, the scanner has read something it should not
// have.
var secretSentinels = []string{
	"sk-SECRET-MUST-NOT-APPEAR",
	"SECRET-MUST-NOT-APPEAR",
	"/Users/tester/Secrets",
}

// want is one expected finding, described by the parts a user would check.
type want struct {
	kind      model.Kind
	name      string
	client    string
	scope     model.Scope
	publisher string
	version   string
	reach     []model.Reach
	notes     []string // substrings, each must appear in some note
	command   string   // substring of Command
}

type scanCase struct {
	name string
	os   string
	// fixture is the directory under testdata holding home, sys and root.
	fixture   string
	withRoot  bool
	want      []want
	absent    []string // finding names that must not appear
	errSubstr []string // substrings that must appear in some scan error
	noErrors  bool
}

func TestScan(t *testing.T) {
	cases := []scanCase{
		{
			name:    "macos machine with extensions plugins skills connectors and a scheduled agent",
			os:      "darwin",
			fixture: "macos",
			want: []want{
				{
					kind: model.KindExtension, name: "Control your Mac", client: clientClaudeDesktop,
					scope: model.ScopeUser, publisher: "Kenneth Example", version: "0.0.1",
					reach: []model.Reach{model.ReachAppleScript, model.ReachShell},
					notes: []string{"signature: signed", "signature publisher: Example Publisher CA", "enabled in the client", "dxt_version 0.1"},
				},
				{
					kind: model.KindExtension, name: "Shell Runner", client: clientClaudeDesktop,
					scope: model.ScopeUser, publisher: "Example Tools Ltd", version: "2.1.0",
					reach:   []model.Reach{model.ReachFilesystem, model.ReachShell},
					notes:   []string{"signature: unsigned", "present but switched off in the client", "api_key (string, required, marked sensitive)"},
					command: "/bin/bash",
				},
				{
					// The author here is a bare string rather than an object.
					kind: model.KindExtension, name: "remote-caller", publisher: "Solo Author", version: "0.4.2",
					reach: []model.Reach{model.ReachBrowserTabs, model.ReachNetwork, model.ReachShell},
					notes: []string{"signature: not recorded by the client", "the client has no install record"},
				},
				{
					kind: model.KindPlugin, name: "demo-plugin", client: clientClaudeCode,
					scope: model.ScopeUser, publisher: "Demo Author", version: "1.2.0",
					reach: []model.Reach{model.ReachShell},
					notes: []string{
						"installed from marketplace demo-market (github:example/demo-market)",
						"pinned to commit 2ab958093e83e0ec752e6c1c5932da465bf23e0c",
						"present but switched off in settings.json",
						"event hooks",
					},
				},
				{
					kind: model.KindPlugin, name: "local-plugin", scope: model.ScopeUser,
					notes: []string{"installed from a local path, not a marketplace"},
				},
				{
					kind: model.KindSkill, name: "tdd", client: clientClaudeCode, scope: model.ScopeUser,
					publisher: "Demo Author",
					reach:     []model.Reach{model.ReachFilesystem, model.ReachShell},
					notes: []string{
						"installed with plugin demo-plugin from marketplace demo-market",
						"declares allowed tools: Bash(go test:*), Read, Edit",
						"ships 1 executable script file alongside",
					},
				},
				{
					kind: model.KindSkill, name: "local-only",
					notes: []string{"installed with plugin local-plugin, installed from a local path"},
				},
				{
					kind: model.KindSkill, name: "planit", scope: model.ScopeUser, version: "3",
					reach: []model.Reach{model.ReachUnknown},
					notes: []string{"written or placed here by the user", "declares no tool restriction"},
				},
				{
					// No front matter at all: the folder name is the name.
					kind: model.KindSkill, name: "no-front-matter", scope: model.ScopeUser,
				},
				{
					kind: model.KindConnector, name: "team-hub", client: clientClaudeDesktop,
					scope: model.ScopeUser, reach: []model.Reach{model.ReachNetwork},
					command: "https://hub.example.com/mcp",
					notes:   []string{"host: hub.example.com", "sends Authorization (header values are not read"},
				},
				{
					kind: model.KindConnector, name: "notes-connector",
					notes: []string{"declared under connectors as a remote endpoint"},
				},
				{
					kind: model.KindScheduledTask, name: "com.example.claude-nightly", client: clientClaudeCode,
					scope: model.ScopeUser, command: "/usr/local/bin/claude -p review the inbox",
					notes: []string{"runs agent binary: claude", "starts on: StartCalendarInterval"},
				},
				{
					kind: model.KindScheduledTask, name: "com.example.codex", client: "Codex CLI",
					scope: model.ScopeSystem, command: "/opt/homebrew/bin/codex",
					notes: []string{"starts on: StartInterval=3600"},
				},
			},
			absent: []string{
				"local-thing",         // a local server is not a connector
				"com.example.wrapper", // conservative match: a bash wrapper is missed
				"ignored",             // a skill vendored inside node_modules
				"com.vendor.printer",  // an unparsed plist that names no agent
			},
			errSubstr: []string{
				"com.example.broken",   // malformed manifest json
				"has no manifest.json", // an install directory with nothing in it
				"binary property list mentions claude",
			},
		},
		{
			name:    "linux machine with a gemini extension a systemd unit and cron",
			os:      "linux",
			fixture: "linux",
			want: []want{
				{
					kind: model.KindExtension, name: "workspace", client: clientGeminiCLI,
					scope: model.ScopeUser, version: "1.4.0",
					reach: []model.Reach{model.ReachNetwork, model.ReachShell},
					notes: []string{"declares model context protocol servers: hosted, workspace", "loads context file: GEMINI.md", "declares excluded tools: run_shell_command"},
				},
				{
					kind: model.KindScheduledTask, name: "claude-nightly", client: clientClaudeCode,
					scope: model.ScopeUser,
					notes: []string{"started by claude-nightly.timer", "runs agent binary: claude"},
				},
				{
					kind: model.KindScheduledTask, name: "codex in crontab", client: "Codex CLI",
					scope: model.ScopeSystem,
					notes: []string{"cron entry, schedule: 0 3 * * * root"},
				},
				{
					kind: model.KindScheduledTask, name: "gemini in agents", client: clientGeminiCLI,
					notes: []string{"cron entry, schedule: @daily tester"},
				},
			},
			absent: []string{"unrelated", "rsync in crontab"},
		},
		{
			name:    "windows machine with an extension a scheduled task and a startup shortcut",
			os:      "windows",
			fixture: "windows",
			want: []want{
				{
					kind: model.KindExtension, name: "Windows Helper", client: clientClaudeDesktop,
					publisher: "Windows Example", version: "3.0.0",
					reach: []model.Reach{model.ReachFilesystem, model.ReachShell},
					notes: []string{"signature: self-signed", "install source: sideload"},
				},
				{
					kind: model.KindScheduledTask, name: "AgentNightly", client: clientClaudeCode,
					scope: model.ScopeSystem, publisher: `EXAMPLE\tester`,
					notes: []string{"runs at logon, on a calendar schedule", "runs agent binary: claude"},
				},
				{
					// Written as real UTF-16, the way Windows stores them.
					kind: model.KindScheduledTask, name: "GeminiDigest", client: clientGeminiCLI,
					notes: []string{"runs at a set time"},
				},
				{
					kind: model.KindScheduledTask, name: "claude.lnk", client: clientClaudeCode,
					scope: model.ScopeUser,
					notes: []string{"runs at login", "the shortcut target is not read"},
				},
			},
			absent:    []string{"notepad.lnk", "Refresh"},
			errSubstr: []string{"BrokenTask"},
		},
		{
			name:     "a project directory that carries its own agent machinery",
			os:       "darwin",
			fixture:  "project",
			withRoot: true,
			want: []want{
				{
					kind: model.KindPlugin, name: "repo-plugin", scope: model.ScopeProject,
					publisher: "Repo Owner", version: "0.9.0",
					notes: []string{"defined in this project directory, not installed from a marketplace"},
				},
				{
					kind: model.KindPlugin, name: "repo-market", scope: model.ScopeProject,
					notes: []string{"a plugin marketplace defined in this project, offering 2 plugins"},
				},
				{
					kind: model.KindSkill, name: "repo-skill", scope: model.ScopeProject,
					reach: []model.Reach{model.ReachNetwork, model.ReachShell},
					notes: []string{"carried by this project directory, not installed by the user"},
				},
				{
					kind: model.KindConnector, name: "repo-remote", client: clientClaudeCode,
					scope: model.ScopeProject, command: "https://repo.example.com/sse",
					notes: []string{"declares environment variables: TOKEN (values are not read"},
				},
			},
			absent: []string{"repo-local"},
		},
		{
			name:     "a machine with none of it installed",
			os:       "darwin",
			fixture:  "empty",
			withRoot: true,
			noErrors: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, sys, root := prepare(t, tc.fixture)
			env := model.Env{OS: tc.os, HomeDir: home}
			if tc.withRoot {
				env.Roots = []string{root}
			}

			findings, gaps, errs := scanner{sysRoot: sys}.Scan(env)

			if len(gaps) == 0 {
				t.Error("a run that records no blind spots is claiming it looked everywhere")
			}

			for _, w := range tc.want {
				checkFinding(t, findings, w)
			}
			for _, name := range tc.absent {
				if f := lookup(findings, "", name); f != nil {
					t.Errorf("found %q (%s at %s), which this scanner should not report", name, f.Kind, f.Source)
				}
			}
			for _, substr := range tc.errSubstr {
				if !anyError(errs, substr) {
					t.Errorf("expected a scan error mentioning %q, got %v", substr, errs)
				}
			}
			if tc.noErrors {
				if len(errs) != 0 {
					t.Errorf("an empty machine should produce no errors, got %v", errs)
				}
				if len(findings) != 0 {
					t.Errorf("an empty machine should produce no findings, got %d", len(findings))
				}
			}

			checkNoSecrets(t, findings)
			checkWellFormed(t, findings)
		})
	}
}

// TestScanIsRepeatable is what drift detection depends on: the same machine
// scanned twice must produce the same digests.
func TestScanIsRepeatable(t *testing.T) {
	home, sys, _ := prepare(t, "macos")
	env := model.Env{OS: "darwin", HomeDir: home}

	first, _, _ := scanner{sysRoot: sys}.Scan(env)
	second, _, _ := scanner{sysRoot: sys}.Scan(env)

	if len(first) != len(second) {
		t.Fatalf("two runs found different counts: %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Name != second[i].Name || first[i].Digest != second[i].Digest {
			t.Errorf("run differed at %d: %s/%s then %s/%s",
				i, first[i].Name, first[i].Digest, second[i].Name, second[i].Digest)
		}
	}
}

// TestDigestFollowsTheDeclaration is the rug-pull case: an extension the user
// already approved, quietly redefined.
func TestDigestFollowsTheDeclaration(t *testing.T) {
	home, sys, _ := prepare(t, "macos")
	env := model.Env{OS: "darwin", HomeDir: home}

	before, _, _ := scanner{sysRoot: sys}.Scan(env)
	was := lookup(before, model.KindExtension, "Control your Mac")
	if was == nil {
		t.Fatal("fixture extension missing")
	}

	manifest := filepath.Join(home, "Library", "Application Support", "Claude",
		"Claude Extensions", "com.example.osascript", "manifest.json")
	b, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// The name and version stay the same; only what it runs changes.
	changed := strings.Replace(string(b), `"server/index.js"`, `"server/other.js"`, -1)
	if changed == string(b) {
		t.Fatal("fixture did not contain the entry point being changed")
	}
	if err := os.WriteFile(manifest, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}

	after, _, _ := scanner{sysRoot: sys}.Scan(env)
	now := lookup(after, model.KindExtension, "Control your Mac")
	if now == nil {
		t.Fatal("extension disappeared after the manifest changed")
	}
	if now.Digest == was.Digest {
		t.Error("the entry point changed and the digest did not, so drift would be missed")
	}
}

// TestBundleOnDisk covers a packaged extension that has been downloaded but not
// installed, and a bundle that cannot be opened.
func TestBundleOnDisk(t *testing.T) {
	home := t.TempDir()
	downloads := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{
	  "manifest_version": "0.3",
	  "name": "bundled-helper",
	  "display_name": "Bundled Helper",
	  "version": "1.0.0",
	  "author": {"name": "Bundle Author"},
	  "server": {"type": "node", "entry_point": "index.js",
	             "mcp_config": {"command": "node", "args": ["${__dirname}/index.js"]}},
	  "tools": [{"name": "read_file", "description": "Read a file."}]
	}`
	writeBundle(t, filepath.Join(downloads, "helper.mcpb"), manifest)
	writeBundle(t, filepath.Join(downloads, "legacy.dxt"), manifest)

	// A file with the right name that is not a zip at all.
	if err := os.WriteFile(filepath.Join(downloads, "corrupt.mcpb"), []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A zip with no manifest in it.
	writeZip(t, filepath.Join(downloads, "empty.dxt"), map[string]string{"README.md": "nothing here"})

	findings, _, errs := scanner{}.Scan(model.Env{OS: "darwin", HomeDir: home})

	found := 0
	for _, f := range findings {
		if f.Name == "Bundled Helper" {
			found++
			if !hasNote(f, "packaged bundle on disk") {
				t.Errorf("bundle finding did not say it is a bundle: %v", f.Notes)
			}
			if !hasReach(f, model.ReachShell) || !hasReach(f, model.ReachFilesystem) {
				t.Errorf("bundle reach = %v, want shell and filesystem", f.Reach)
			}
		}
	}
	if found != 2 {
		t.Errorf("found %d bundles, want 2 (one .mcpb and one .dxt)", found)
	}
	if !anyError(errs, "corrupt.mcpb") {
		t.Errorf("a bundle that will not open must be reported, got %v", errs)
	}
	if !anyError(errs, "no manifest.json") {
		t.Errorf("a bundle with no manifest must be reported, got %v", errs)
	}
}

// TestUnreadableDirectory covers permission denied, which must be reported
// rather than silently turning into an empty inventory.
func TestUnreadableDirectory(t *testing.T) {
	// os.Chmod cannot make a directory unlistable on Windows, where permissions
	// are ACLs and Chmod only touches the read-only attribute. The directory
	// stayed readable, so the scanner had nothing to report and the test failed
	// on the simulation rather than on the behaviour. Skipped honestly; the Unix
	// runs still cover the code path.
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions are ACL-based; os.Chmod cannot make a directory unlistable")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read anything")
	}
	home := t.TempDir()
	skills := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(skills, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skills, 0o755) })

	_, _, errs := scanner{}.Scan(model.Env{OS: "darwin", HomeDir: home})
	if !anyError(errs, filepath.Join(".claude", "skills")) {
		t.Errorf("an unreadable directory must be reported, got %v", errs)
	}
}

// TestBadInput throws shapes at the scanner that a real machine can produce.
// None of them may panic and none may abort the run.
func TestBadInput(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), `{"plugins": {"a@b": [{"installPath": ""}]}}`)
	mustWrite(t, filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"), `[]`)
	mustWrite(t, filepath.Join(home, ".claude", "settings.json"), `null`)
	mustWrite(t, filepath.Join(home, ".claude", "skills", "odd", "SKILL.md"), "---\nname:\n---\n")
	mustWrite(t, filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), `{"mcpServers": "not an object"}`)
	mustWrite(t, filepath.Join(home, ".gemini", "extensions", "x", "gemini-extension.json"), `{"mcpServers": {"a": {"url": 5}}}`)
	mustWrite(t, filepath.Join(home, "Library", "LaunchAgents", "empty.plist"), "")

	findings, gaps, errs := scanner{}.Scan(model.Env{OS: "darwin", HomeDir: home, Roots: []string{home}})

	if len(gaps) == 0 {
		t.Error("even a broken machine must state its blind spots")
	}
	if len(errs) == 0 {
		t.Error("malformed files must show up as errors rather than vanish")
	}
	checkWellFormed(t, findings)
}

func TestParseCrontab(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		hasUser bool
		want    []cronEntry
	}{
		{
			name: "user crontab",
			text: "# comment\nPATH=/usr/bin\n0 2 * * * /usr/local/bin/claude -p go\n",
			want: []cronEntry{{schedule: "0 2 * * *", command: "/usr/local/bin/claude -p go"}},
		},
		{
			name:    "system crontab carries a user field",
			text:    "0 3 * * * root /usr/local/bin/codex exec\n",
			hasUser: true,
			want:    []cronEntry{{schedule: "0 3 * * * root", command: "/usr/local/bin/codex exec"}},
		},
		{
			name:    "shorthand schedule",
			text:    "@reboot tester /usr/local/bin/gemini\n",
			hasUser: true,
			want:    []cronEntry{{schedule: "@reboot tester", command: "/usr/local/bin/gemini"}},
		},
		{name: "nothing but noise", text: "\n# only a comment\nSHELL=/bin/sh\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCrontab(tc.text, tc.hasUser)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d entries %v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestReachIsDerivedNotGuessed(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		args []string
		want []model.Reach
	}{
		{name: "a shell entry point", cmd: "/bin/zsh", want: []model.Reach{model.ReachShell}},
		{name: "an interpreter entry point", cmd: "node", want: []model.Reach{model.ReachShell}},
		{name: "windows path and extension", cmd: `C:\Program Files\nodejs\node.exe`, want: []model.Reach{model.ReachShell}},
		{name: "osascript", cmd: "/usr/bin/osascript", want: []model.Reach{model.ReachAppleScript, model.ReachShell}},
		{name: "a declared endpoint", cmd: "./server", args: []string{"--url", "https://api.example.com"}, want: []model.Reach{model.ReachNetwork}},
		{name: "a bundled binary says nothing", cmd: "./bin/server", want: []model.Reach{model.ReachUnknown}},
		{name: "nothing declared at all", want: []model.Reach{model.ReachUnknown}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := newReachSet()
			rs.fromCommand(tc.cmd, tc.args)
			got := rs.list()
			if len(got) != len(tc.want) {
				t.Fatalf("reach = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("reach = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestParseFrontMatter(t *testing.T) {
	got := parseFrontMatter([]byte("---\r\nname: thing\r\ndescription: \"quoted\"\r\nallowed-tools: Bash, Read\r\nnested:\r\n  key: value\r\n---\r\n# body\r\n"))
	if got["name"] != "thing" {
		t.Errorf("name = %q", got["name"])
	}
	if got["description"] != "quoted" {
		t.Errorf("description = %q", got["description"])
	}
	if got["allowed-tools"] != "Bash, Read" {
		t.Errorf("allowed-tools = %q", got["allowed-tools"])
	}
	if len(parseFrontMatter([]byte("no front matter"))) != 0 {
		t.Error("a file with no front matter must yield nothing")
	}
	if len(parseFrontMatter([]byte("---\nname: unterminated\n"))) != 0 {
		t.Error("an unterminated front matter block must yield nothing")
	}
}

// ---------------------------------------------------------------- helpers

// prepare copies a fixture into a temporary directory and fills in the absolute
// paths that only exist once the copy has a home. Copying keeps a test that
// writes (the drift test) from editing the checked in fixtures.
func prepare(t *testing.T, fixture string) (home, sys, root string) {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("testdata", fixture)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fixture %s: %v", fixture, err)
	}
	copyTree(t, src, dst)

	home = filepath.Join(dst, "home")
	sys = filepath.Join(dst, "sys")
	root = filepath.Join(dst, "root")

	substitute(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), map[string]string{
		"PLUGIN_INSTALL_PATH": filepath.Join(home, ".claude", "plugins", "cache", "demo-market", "demo-plugin", "1.2.0"),
		"LOCAL_PLUGIN_PATH":   filepath.Join(home, ".claude", "plugins", "cache", "local", "local-plugin"),
	})
	substitute(t, filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"), map[string]string{
		"MARKET_PATH": filepath.Join(home, ".claude", "plugins", "marketplaces", "demo-market"),
	})
	return home, sys, root
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
	if err != nil {
		t.Fatalf("copying fixture: %v", err)
	}
}

// substitute rewrites the path placeholders in a JSON fixture to point at the
// temporary copy. The replacement is JSON encoded rather than pasted in raw:
// a Windows path is full of backslashes, and a raw C:\Users\... dropped into a
// JSON string is an invalid escape sequence. Splicing it in unescaped left
// installed_plugins.json unparseable on Windows, so the scanner found no
// plugins and no plugin skills at all while every assertion still passed on
// Linux and macOS.
func substitute(t *testing.T, path string, replacements map[string]string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return // fixtures that do not have this file are fine
	}
	text := string(b)
	for from, to := range replacements {
		text = strings.ReplaceAll(text, from, jsonStringBody(t, to))
	}
	if !json.Valid([]byte(text)) {
		t.Fatalf("substitution left %s invalid JSON:\n%s", path, text)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// jsonStringBody returns s encoded as JSON with the surrounding quotes removed,
// so it can be dropped into a quoted placeholder inside a fixture.
func jsonStringBody(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b[1 : len(b)-1])
}

func lookup(findings []model.Finding, kind model.Kind, name string) *model.Finding {
	for i := range findings {
		if findings[i].Name == name && (kind == "" || findings[i].Kind == kind) {
			return &findings[i]
		}
	}
	return nil
}

func hasReach(f model.Finding, r model.Reach) bool {
	for _, got := range f.Reach {
		if got == r {
			return true
		}
	}
	return false
}

func hasNote(f model.Finding, substr string) bool {
	for _, n := range f.Notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}

func anyError(errs []model.ScanError, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e.Path, substr) || strings.Contains(e.Err, substr) {
			return true
		}
	}
	return false
}

func checkFinding(t *testing.T, findings []model.Finding, w want) {
	t.Helper()
	f := lookup(findings, w.kind, w.name)
	if f == nil {
		t.Errorf("no %s named %q in the inventory", w.kind, w.name)
		return
	}
	if w.client != "" && f.Client != w.client {
		t.Errorf("%s: client = %q, want %q", w.name, f.Client, w.client)
	}
	if w.scope != "" && f.Scope != w.scope {
		t.Errorf("%s: scope = %q, want %q", w.name, f.Scope, w.scope)
	}
	if w.publisher != "" && f.Publisher != w.publisher {
		t.Errorf("%s: publisher = %q, want %q", w.name, f.Publisher, w.publisher)
	}
	if w.version != "" && f.Version != w.version {
		t.Errorf("%s: version = %q, want %q", w.name, f.Version, w.version)
	}
	if w.command != "" && !strings.Contains(f.Command, w.command) {
		t.Errorf("%s: command = %q, want it to contain %q", w.name, f.Command, w.command)
	}
	if len(w.reach) > 0 {
		if len(f.Reach) != len(w.reach) {
			t.Errorf("%s: reach = %v, want %v", w.name, f.Reach, w.reach)
		} else {
			for i := range w.reach {
				if f.Reach[i] != w.reach[i] {
					t.Errorf("%s: reach = %v, want %v", w.name, f.Reach, w.reach)
					break
				}
			}
		}
	}
	for _, note := range w.notes {
		if !hasNote(*f, note) {
			t.Errorf("%s: no note containing %q, notes are %v", w.name, note, f.Notes)
		}
	}
}

// checkWellFormed asserts the parts of a finding that every finding must have,
// whatever it describes.
func checkWellFormed(t *testing.T, findings []model.Finding) {
	t.Helper()
	for _, f := range findings {
		if f.Kind == "" || f.Name == "" || f.Scope == "" {
			t.Errorf("incomplete finding: %+v", f)
		}
		if f.Source == "" || !filepath.IsAbs(f.Source) {
			t.Errorf("%s: source %q is not an absolute path", f.Name, f.Source)
		}
		if f.Digest == "" {
			t.Errorf("%s: no digest, so drift cannot be detected", f.Name)
		}
		if len(f.Reach) == 0 {
			t.Errorf("%s: no reach recorded, which should be %q rather than empty", f.Name, model.ReachUnknown)
		}
	}
}

func checkNoSecrets(t *testing.T, findings []model.Finding) {
	t.Helper()
	b, err := json.Marshal(findings)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secretSentinels {
		if strings.Contains(string(b), secret) {
			t.Errorf("a configured value reached the output: %q", secret)
		}
	}
}

func writeBundle(t *testing.T, path, manifest string) {
	t.Helper()
	writeZip(t, path, map[string]string{"manifest.json": manifest, "index.js": "// server"})
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, name := range sortedKeys(files) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
