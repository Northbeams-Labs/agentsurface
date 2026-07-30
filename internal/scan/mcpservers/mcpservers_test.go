package mcpservers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// want is one expected finding. Only the fields worth pinning are listed; a
// zero field is not compared.
type want struct {
	name    string
	client  string
	scope   model.Scope
	command string
	reach   []model.Reach
	notes   []string // substrings that must each appear in some note
}

// scanFixture runs the scanner against a fixture home with no project roots of
// its own, so that only user scope config is read.
func scanFixture(t *testing.T, home, goos string) ([]model.Finding, []model.Gap, []model.ScanError) {
	t.Helper()
	env := model.Env{
		OS:      goos,
		HomeDir: filepath.Join("testdata", home),
		Roots:   []string{t.TempDir()},
	}
	return New().Scan(env)
}

func TestScanUserScope(t *testing.T) {
	cases := []struct {
		fixture string
		goos    string
		want    []want
	}{
		{
			fixture: "claude_desktop",
			goos:    "darwin",
			want: []want{
				{
					name: "apple-notes", client: "Claude Desktop", scope: model.ScopeUser,
					command: `/usr/bin/osascript -e "tell application \"Notes\" to get every note"`,
					reach:   []model.Reach{model.ReachAppleScript},
				},
				{
					name: "filesystem", client: "Claude Desktop", scope: model.ScopeUser,
					command: "npx -y @modelcontextprotocol/server-filesystem /Users/example/Documents",
					reach:   []model.Reach{model.ReachShell},
				},
				{
					name: "github", client: "Claude Desktop", scope: model.ScopeUser,
					command: "docker run -i --rm -e GITHUB_PERSONAL_ACCESS_TOKEN ghcr.io/github/github-mcp-server",
					reach:   []model.Reach{model.ReachCredentials, model.ReachShell},
					notes:   []string{"GITHUB_PERSONAL_ACCESS_TOKEN", "values are not read"},
				},
			},
		},
		{
			fixture: "claude_code",
			goos:    "darwin",
			want: []want{
				{
					name: "design", client: "Claude Code", scope: model.ScopeUser,
					command: "https://api.example.com/v1/design/mcp",
					reach:   []model.Reach{model.ReachNetwork},
					notes:   []string{"declared transport: http"},
				},
				{
					name: "postgres", client: "Claude Code", scope: model.ScopeProject,
					command: "uvx mcp-server-postgres",
					reach:   []model.Reach{model.ReachShell},
					notes:   []string{"/Users/example/work/api", "DATABASE_URL"},
				},
			},
		},
		{
			fixture: "cursor",
			goos:    "darwin",
			want: []want{
				{
					name: "linear", client: "Cursor", scope: model.ScopeUser,
					command: "https://mcp.example.com/sse",
					reach:   []model.Reach{model.ReachCredentials, model.ReachNetwork},
					notes:   []string{"sends request headers Authorization"},
				},
				{
					name: "local-tools", client: "Cursor", scope: model.ScopeUser,
					command: "bash -lc ./run-mcp.sh",
					reach:   []model.Reach{model.ReachShell},
				},
			},
		},
		{
			// This fixture is also the comments-in-JSON case: the VS Code file
			// carries // and /* */ comments and a trailing comma.
			fixture: "vscode",
			goos:    "darwin",
			want: []want{
				{
					name: "github", client: "VS Code (GitHub Copilot)", scope: model.ScopeUser,
					command: "https://api.githubcopilot.com/mcp",
					reach:   []model.Reach{model.ReachNetwork},
				},
				{
					name: "legacy-placement", client: "VS Code (GitHub Copilot)", scope: model.ScopeUser,
					command: "python3 -m legacy_mcp",
					reach:   []model.Reach{model.ReachShell},
				},
				{
					name: "playwright", client: "VS Code (GitHub Copilot)", scope: model.ScopeUser,
					command: "npx -y @microsoft/mcp-server-playwright",
					reach:   []model.Reach{model.ReachShell},
				},
				{
					name: "profile-only", client: "VS Code (GitHub Copilot)", scope: model.ScopeUser,
					command: "node /opt/tools/index.js",
					reach:   []model.Reach{model.ReachShell},
				},
			},
		},
		{
			fixture: "windsurf",
			goos:    "darwin",
			want: []want{
				{
					name: "sequential-thinking", client: "Windsurf", scope: model.ScopeUser,
					command: "npx -y @modelcontextprotocol/server-sequential-thinking",
					reach:   []model.Reach{model.ReachShell},
				},
			},
		},
		{
			fixture: "zed",
			goos:    "darwin",
			want: []want{
				{
					name: "local-mcp-server", client: "Zed", scope: model.ScopeUser,
					command: "some-command arg-1 arg-2",
					reach:   []model.Reach{model.ReachCredentials, model.ReachUnknown},
					notes:   []string{"API_TOKEN"},
				},
				{
					name: "older-object-command", client: "Zed", scope: model.ScopeUser,
					command: "node /opt/zed-mcp/server.js",
					reach:   []model.Reach{model.ReachShell},
				},
				{
					name: "remote-mcp-server", client: "Zed", scope: model.ScopeUser,
					command: "https://example.com/mcp",
					reach:   []model.Reach{model.ReachCredentials, model.ReachNetwork},
				},
			},
		},
		{
			fixture: "cline",
			goos:    "darwin",
			want: []want{
				{
					name: "weather", client: "Cline in VS Code", scope: model.ScopeUser,
					command: "node /opt/weather/index.js",
					reach:   []model.Reach{model.ReachShell},
					notes:   []string{"switched off", "get_forecast"},
				},
			},
		},
		{
			fixture: "continue",
			goos:    "darwin",
			want: []want{
				{
					name: "sqlite", client: "Continue", scope: model.ScopeUser,
					command: "npx -y mcp-sqlite /Users/example/db.sqlite",
					reach:   []model.Reach{model.ReachShell},
				},
				{
					// The deprecated config.json shape carries no server name,
					// so the command stands in for one.
					name: "uvx", client: "Continue", scope: model.ScopeUser,
					command: "uvx mcp-server-fetch",
					reach:   []model.Reach{model.ReachShell},
				},
			},
		},
		{
			fixture: "jetbrains",
			goos:    "darwin",
			want: []want{
				{
					name: "memory", client: "JetBrains AI Assistant", scope: model.ScopeUser,
					command: "npx -y @modelcontextprotocol/server-memory",
					reach:   []model.Reach{model.ReachShell},
				},
			},
		},
		{
			fixture: "gemini",
			goos:    "darwin",
			want: []want{
				{
					name: "github", client: "Gemini CLI", scope: model.ScopeUser,
					command: "npx -y @modelcontextprotocol/server-github",
					reach:   []model.Reach{model.ReachShell},
				},
			},
		},
		{
			fixture: "linuxhome",
			goos:    "linux",
			want: []want{
				{
					name: "fetch", client: "Claude Desktop", scope: model.ScopeUser,
					command: "uvx mcp-server-fetch",
					reach:   []model.Reach{model.ReachShell},
				},
			},
		},
		{
			fixture: "windowshome",
			goos:    "windows",
			want: []want{
				{
					name: "windows-fs", client: "Claude Desktop", scope: model.ScopeUser,
					command: `cmd.exe /c npx -y @modelcontextprotocol/server-filesystem C:\Users\example`,
					reach:   []model.Reach{model.ReachShell},
				},
			},
		},
		{
			// An empty machine finds nothing and still reports its blind spots.
			fixture: "empty",
			goos:    "darwin",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			findings, gaps, errs := scanFixture(t, tc.fixture, tc.goos)
			if len(errs) != 0 {
				t.Fatalf("unexpected scan errors: %+v", errs)
			}
			if len(gaps) == 0 {
				t.Error("every run must state what it did not look at, got no gaps")
			}
			checkFindings(t, findings, tc.want)
		})
	}
}

func checkFindings(t *testing.T, got []model.Finding, expected []want) {
	t.Helper()

	if len(got) != len(expected) {
		t.Fatalf("found %d servers, want %d\ngot: %s", len(got), len(expected), summarise(got))
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Name < got[j].Name })
	sorted := append([]want(nil), expected...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })

	for i, w := range sorted {
		f := got[i]
		if f.Kind != model.KindMCPServer {
			t.Errorf("%s: kind = %q, want %q", f.Name, f.Kind, model.KindMCPServer)
		}
		if f.Name != w.name {
			t.Errorf("finding %d: name = %q, want %q", i, f.Name, w.name)
			continue
		}
		if f.Client != w.client {
			t.Errorf("%s: client = %q, want %q", w.name, f.Client, w.client)
		}
		if f.Scope != w.scope {
			t.Errorf("%s: scope = %q, want %q", w.name, f.Scope, w.scope)
		}
		if f.Command != w.command {
			t.Errorf("%s: command = %q, want %q", w.name, f.Command, w.command)
		}
		if !reflect.DeepEqual(f.Reach, w.reach) {
			t.Errorf("%s: reach = %v, want %v", w.name, f.Reach, w.reach)
		}
		if !filepath.IsAbs(f.Source) {
			t.Errorf("%s: source %q is not absolute", w.name, f.Source)
		}
		if !strings.HasPrefix(f.Digest, "sha256:") || len(f.Digest) != len("sha256:")+64 {
			t.Errorf("%s: digest = %q, want a sha256 hex digest", w.name, f.Digest)
		}
		joined := strings.Join(f.Notes, " | ")
		for _, n := range w.notes {
			if !strings.Contains(joined, n) {
				t.Errorf("%s: notes %q missing %q", w.name, joined, n)
			}
		}
	}
}

func summarise(findings []model.Finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("\n  " + f.Client + " / " + f.Name + " -> " + f.Source)
	}
	return b.String()
}

func TestProjectScope(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("testdata", "project", "repo"))
	if err != nil {
		t.Fatal(err)
	}
	env := model.Env{
		OS:      "darwin",
		HomeDir: filepath.Join("testdata", "empty"),
		Roots:   []string{root},
	}
	findings, _, errs := New().Scan(env)
	if len(errs) != 0 {
		t.Fatalf("unexpected scan errors: %+v", errs)
	}

	checkFindings(t, findings, []want{
		{name: "nested-package", client: "Claude Code", scope: model.ScopeProject,
			command: "uvx nested-mcp", reach: []model.Reach{model.ReachShell}},
		{name: "repo-cursor", client: "Cursor", scope: model.ScopeProject,
			command: "node ./tools/mcp.js", reach: []model.Reach{model.ReachShell}},
		{name: "repo-junie", client: "JetBrains Junie", scope: model.ScopeProject,
			command: "npx -y @example/junie-mcp", reach: []model.Reach{model.ReachShell}},
		{name: "repo-shared", client: "Claude Code", scope: model.ScopeProject,
			command: "npx -y @example/repo-mcp", reach: []model.Reach{model.ReachShell}},
		{name: "repo-vscode", client: "VS Code (GitHub Copilot)", scope: model.ScopeProject,
			command: "https://example.com/sse", reach: []model.Reach{model.ReachNetwork}},
	})

	// The walk is bounded: nothing from node_modules, from a hidden directory,
	// or from below the depth limit.
	for _, f := range findings {
		switch f.Name {
		case "vendored", "inside-git", "too-deep":
			t.Errorf("walk reached %q at %s, which is outside the shallow bounded walk", f.Name, f.Source)
		}
	}
}

func TestProjectScopeDoesNotFollowSymlinksOutOfRoot(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, ".mcp.json"),
		[]byte(`{"mcpServers":{"outside-the-root":{"command":"uvx","args":["x"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, ".mcp.json"), filepath.Join(root, ".mcp.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	env := model.Env{OS: "darwin", HomeDir: filepath.Join("testdata", "empty"), Roots: []string{root}}
	findings, _, _ := New().Scan(env)
	for _, f := range findings {
		t.Errorf("followed a symlink out of the root and found %q at %s", f.Name, f.Source)
	}
}

func TestMalformedConfigBecomesAScanError(t *testing.T) {
	findings, gaps, errs := scanFixture(t, "malformed", "darwin")
	if len(findings) != 0 {
		t.Errorf("malformed config produced %d findings, want 0", len(findings))
	}
	if len(gaps) == 0 {
		t.Error("a failed parse must not stop the run reporting its gaps")
	}
	if len(errs) != 1 {
		t.Fatalf("errors = %+v, want exactly one", errs)
	}
	if errs[0].Scanner != "mcpservers" {
		t.Errorf("scanner = %q, want mcpservers", errs[0].Scanner)
	}
	if !strings.HasSuffix(errs[0].Path, filepath.Join(".cursor", "mcp.json")) {
		t.Errorf("path = %q, want the cursor config", errs[0].Path)
	}
	if errs[0].Err == "" {
		t.Error("scan error carries no explanation")
	}
}

func TestUnreadableConfigBecomesAScanError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can read a mode 0000 file")
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o000); err != nil {
		t.Fatal(err)
	}

	env := model.Env{OS: "darwin", HomeDir: home, Roots: []string{t.TempDir()}}
	findings, _, errs := New().Scan(env)
	if len(findings) != 0 {
		t.Errorf("findings = %d, want 0", len(findings))
	}
	if len(errs) != 1 || errs[0].Err != "permission denied" {
		t.Fatalf("errors = %+v, want one permission denied", errs)
	}
}

// TestSecretsNeverLeave is the rule the whole package is built around: an env
// value can be an API key, so it must not appear in a finding, a note, a
// command line, a digest or a gap.
func TestSecretsNeverLeave(t *testing.T) {
	findings, gaps, errs := scanFixture(t, "secrets", "darwin")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}

	blob, err := json.Marshal(struct {
		F []model.Finding
		G []model.Gap
	}{findings, gaps})
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(blob)

	// Values taken verbatim from testdata/secrets/.cursor/mcp.json.
	for _, secret := range []string{
		"example-secret-value-do-not-copy-9f3a", // an env value that looks like a key
		"example-plain-value-do-not-copy-7b21",  // an env value that does not
		"abc123DEF456ghi789jkl012mno345pq",      // --api-key=
		"zzTOPsecret123456789012345678901234",   // --token
		"abc123DEF456ghi789jkl",                 // access_token query parameter
	} {
		if strings.Contains(rendered, secret) {
			t.Errorf("value %q from the config appears in the output", secret)
		}
	}

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	f := findings[0]
	wantCommand := "node /opt/api-mcp/server.js --api-key=[redacted] --token [redacted] https://example.com/mcp?access_token=[redacted]"
	if f.Command != wantCommand {
		t.Errorf("command = %q, want %q", f.Command, wantCommand)
	}
	// The names are reported, because which credentials a server is handed is a
	// fact worth knowing. Only the values are withheld.
	notes := strings.Join(f.Notes, " | ")
	for _, name := range []string{"OPENAI_API_KEY", "PLAIN_SETTING"} {
		if !strings.Contains(notes, name) {
			t.Errorf("notes %q should name the environment variable %q", notes, name)
		}
	}
}

func TestDigestIgnoresEnvValuesAndTracksTheCommand(t *testing.T) {
	base := server{
		name: "api", command: "node", args: []string{"server.js"},
		envKeys: []string{"OPENAI_API_KEY"},
	}
	same := base
	// envKeys is all the digest ever sees, so a rotated secret value cannot
	// change it. Rebuilding the identical declaration must give the same hash.
	same.args = []string{"server.js"}

	if digestOf(base) != digestOf(same) {
		t.Error("the same declaration hashed to two different digests")
	}

	changed := base
	changed.command = "bash"
	if digestOf(base) == digestOf(changed) {
		t.Error("changing the command did not change the digest")
	}

	renamedKey := base
	renamedKey.envKeys = []string{"OPENAI_API_KEY", "GITHUB_TOKEN"}
	if digestOf(base) == digestOf(renamedKey) {
		t.Error("adding an environment variable name did not change the digest")
	}

	withTools := base
	withTools.toolNames = []string{"run_shell"}
	if digestOf(base) == digestOf(withTools) {
		t.Error("declaring a tool did not change the digest")
	}
}

// TestDigestSurvivesARotatedSecret checks the same rule end to end: two configs
// that differ only in an environment variable's value are the same declaration,
// so they must produce the same digest and must not show up as drift.
func TestDigestSurvivesARotatedSecret(t *testing.T) {
	const tmpl = `{"mcpServers":{"api":{"command":"node","args":["s.js"],"env":{"API_KEY":%q}}}}`

	digest := func(value string) string {
		servers, err := shapeMCPServers([]byte(strings.Replace(tmpl, "%q", `"`+value+`"`, 1)))
		if err != nil {
			t.Fatal(err)
		}
		if len(servers) != 1 {
			t.Fatalf("parsed %d servers, want 1", len(servers))
		}
		return digestOf(servers[0])
	}

	if digest("first-value-aaa") != digest("second-value-bbb") {
		t.Error("rotating an environment variable value changed the digest, so a value reached the hash")
	}
}

func TestReachOf(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name string
		in   server
		want []model.Reach
	}{
		{"shell", server{command: "/bin/zsh", args: []string{"-c", "run"}}, []model.Reach{model.ReachShell}},
		{"interpreter", server{command: "npx"}, []model.Reach{model.ReachShell}},
		{"windows shell", server{command: `C:\Windows\System32\cmd.exe`}, []model.Reach{model.ReachShell}},
		{"applescript", server{command: "/usr/bin/osascript"}, []model.Reach{model.ReachAppleScript}},
		{"applescript in args", server{command: "/opt/bin/mcp", args: []string{"--exec", "/usr/bin/osascript"}},
			[]model.Reach{model.ReachAppleScript, model.ReachUnknown}},
		{"remote url", server{url: "https://example.com/mcp"}, []model.Reach{model.ReachNetwork}},
		{"declared sse transport", server{transport: "sse", command: "node"},
			[]model.Reach{model.ReachNetwork, model.ReachShell}},
		{"credentials by name", server{command: "node", envKeys: []string{"SLACK_BOT_TOKEN"}},
			[]model.Reach{model.ReachCredentials, model.ReachShell}},
		{"directory argument", server{command: "npx", args: []string{dir}},
			[]model.Reach{model.ReachFilesystem, model.ReachShell}},
		{"unrecognised binary", server{command: "/opt/vendor/their-mcp-server"}, []model.Reach{model.ReachUnknown}},
		{"nothing declared", server{}, []model.Reach{model.ReachUnknown}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reachOf(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("reach = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRedactArg(t *testing.T) {
	cases := []struct{ arg, prev, want string }{
		{"--api-key=abc123DEF456ghi789jkl012", "", "--api-key=" + redacted},
		{"--token", "", "--token"},
		{"abc123DEF456ghi789jkl012", "--token", redacted},
		{"ghp_abc123", "", redacted},
		{"eyJhbGciOiJIUzI1NiJ9.eyJhIjoxfQ.sig", "", redacted},
		{"-y", "", "-y"},
		{"@modelcontextprotocol/server-filesystem", "", "@modelcontextprotocol/server-filesystem"},
		{"/Users/example/a-fairly-long-directory-name/server.js", "", "/Users/example/a-fairly-long-directory-name/server.js"},
		{"mcp-server-sequential-thinking-long-name", "", "mcp-server-sequential-thinking-long-name"},
		{"https://user:pw@example.com/mcp", "", "https://" + redacted + "@example.com/mcp"},
		{"https://example.com/mcp?api_key=secret123", "", "https://example.com/mcp?api_key=" + redacted},
		{"https://example.com/mcp", "", "https://example.com/mcp"},
	}
	for _, tc := range cases {
		if got := redactArg(tc.arg, tc.prev); got != tc.want {
			t.Errorf("redactArg(%q, %q) = %q, want %q", tc.arg, tc.prev, got, tc.want)
		}
	}
}

func TestStripJSONC(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"line comment", "{\n// a\n\"a\":1\n}", "{\n\n\"a\":1\n}"},
		{"block comment", "{/* a */\"a\":1}", "{\"a\":1}"},
		{"trailing comma object", `{"a":1,}`, `{"a":1}`},
		{"trailing comma array", `{"a":[1,2,]}`, `{"a":[1,2]}`},
		{"slashes inside a string survive", `{"a":"http://x//y"}`, `{"a":"http://x//y"}`},
		{"escaped quote inside a string survives", `{"a":"say \"//\" now"}`, `{"a":"say \"//\" now"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(stripJSONC([]byte(tc.in))); got != tc.want {
				t.Errorf("stripJSONC(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDecodeJSONCRejectsRubbish(t *testing.T) {
	var v map[string]any
	if err := decodeJSONC([]byte(`{"a": }`), &v); err == nil {
		t.Error("broken JSON decoded without an error")
	}
	if err := decodeJSONC([]byte("not json at all"), &v); err == nil {
		t.Error("a non JSON file decoded without an error")
	}
}

// TestClientTableIsSane guards the table itself. A placeholder typo would
// expand to a path that cannot exist, and the scan would quietly report an
// empty machine, which is the failure this package exists to prevent.
func TestClientTableIsSane(t *testing.T) {
	if len(clients) < 25 {
		t.Errorf("client table has only %d rows, which is fewer than the clients this package claims to cover", len(clients))
	}

	seen := map[string]bool{}
	for _, c := range clients {
		if c.name == "" || c.path == "" || c.scope == "" || c.shape == nil {
			t.Errorf("incomplete client row: %+v", c)
		}
		switch c.goos {
		case "", "darwin", "linux", "windows":
		default:
			t.Errorf("%s: unknown goos %q", c.name, c.goos)
		}
		key := c.name + "\x00" + c.goos + "\x00" + c.path
		if seen[key] {
			t.Errorf("duplicate client row: %s %s %s", c.name, c.goos, c.path)
		}
		seen[key] = true

		env := model.Env{OS: c.goos, HomeDir: filepath.Join("testdata", "empty")}
		for _, p := range expandPath(env, c.path) {
			if strings.ContainsAny(p, "{}") {
				t.Errorf("%s: path %q expanded to %q with a placeholder left in it", c.name, c.path, p)
			}
		}
	}

	// Every client the package claims to cover must actually be in the table.
	for _, name := range []string{
		"Claude Desktop", "Claude Code", "Cursor", "VS Code (GitHub Copilot)",
		"Windsurf", "Zed", "Cline", "Cline in VS Code", "Continue",
		"JetBrains AI Assistant", "Gemini CLI",
	} {
		found := false
		for _, c := range clients {
			if c.name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("client %q is claimed but missing from the table", name)
		}
	}
}

// TestEmptyEnvIsSurvivable covers the bad input case: a zero Env has no OS, no
// home and no roots. It must not read anything relative to the working
// directory, must not panic, and must still say what it did not look at.
func TestEmptyEnvIsSurvivable(t *testing.T) {
	// model.Env documents empty Roots as the working directory, so run from an
	// empty one to keep this about the missing home directory.
	t.Chdir(t.TempDir())

	findings, gaps, errs := New().Scan(model.Env{})
	if len(findings) != 0 {
		t.Errorf("a zero Env produced %d findings, want 0: %s", len(findings), summarise(findings))
	}
	if len(errs) != 0 {
		t.Errorf("a zero Env produced errors: %+v", errs)
	}
	joined := ""
	for _, g := range gaps {
		joined += g.Area + ": " + g.Reason + "\n"
	}
	if !strings.Contains(joined, "no home directory was given") {
		t.Errorf("a run with no home directory must say so:\n%s", joined)
	}
}

func TestGapsNameTheBlindSpots(t *testing.T) {
	_, gaps, _ := scanFixture(t, "continue", "darwin")

	joined := ""
	for _, g := range gaps {
		if g.Area == "" || g.Reason == "" {
			t.Errorf("incomplete gap: %+v", g)
		}
		joined += g.Area + ": " + g.Reason + "\n"
	}

	for _, must := range []string{
		"Goose",           // a client that is not covered, named
		"running process", // servers started outside a config file
		"vendor",          // remote and cloud hosted servers
		"config.yaml",     // the Continue YAML file present in this fixture
		"JetBrains",       // the undocumented storage location
	} {
		if !strings.Contains(joined, must) {
			t.Errorf("gaps do not mention %q:\n%s", must, joined)
		}
	}
}
