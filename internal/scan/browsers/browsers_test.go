package browsers

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// scanFixture runs the scanner against a fixture machine. sysSub may be empty,
// in which case machine-wide paths point at an empty directory rather than the
// developer's own root.
func scanFixture(t *testing.T, goos, homeSub, sysSub string) ([]model.Finding, []model.Gap, []model.ScanError) {
	t.Helper()
	home, err := filepath.Abs(filepath.Join("testdata", homeSub))
	if err != nil {
		t.Fatal(err)
	}
	sysRoot := t.TempDir()
	if sysSub != "" {
		if sysRoot, err = filepath.Abs(filepath.Join("testdata", sysSub)); err != nil {
			t.Fatal(err)
		}
	}
	s := scanner{sysRoot: sysRoot}
	return s.Scan(model.Env{OS: goos, HomeDir: home})
}

func byName(findings []model.Finding, name string) *model.Finding {
	for i := range findings {
		if findings[i].Name == name {
			return &findings[i]
		}
	}
	return nil
}

func names(findings []model.Finding, kind model.Kind) []string {
	var out []string
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f.Name)
		}
	}
	return out
}

func hasReach(f *model.Finding, r model.Reach) bool {
	for _, got := range f.Reach {
		if got == r {
			return true
		}
	}
	return false
}

func notesContain(f *model.Finding, want string) bool {
	for _, n := range f.Notes {
		if strings.Contains(n, want) {
			return true
		}
	}
	return false
}

func TestScanMacFixture(t *testing.T) {
	findings, gaps, errs := scanFixture(t, "darwin", "mac", "macsys")

	t.Run("reports the AI extensions and nothing else", func(t *testing.T) {
		want := map[string]bool{
			"Claude":                true, // known id, name and permission shape
			"Monica - AI Assistant": true, // name resolved out of _locales
			"Note Helper":           true, // only signal is its native messaging host
			"Tab Utility Pro":       true, // only signal is the permission shape
			"PagePilot":             true, // Firefox, description match
			"Grammarly for Firefox": true, // Firefox, known id
		}
		got := names(findings, model.KindBrowserExtension)
		for _, name := range got {
			if !want[name] {
				t.Errorf("reported an extension that is not AI-aware: %q", name)
			}
			delete(want, name)
		}
		for name := range want {
			t.Errorf("missing expected extension %q, got %v", name, got)
		}
	})

	t.Run("a password manager and an ad blocker are not reported", func(t *testing.T) {
		for _, name := range []string{"LastPass: Free Password Manager", "Ad Blocker Lite", "uBlock Origin"} {
			if f := byName(findings, name); f != nil {
				t.Errorf("%q was reported as AI-aware: %v", name, f.Notes)
			}
		}
	})

	t.Run("Chromium internal profiles and non-extension add-ons are skipped", func(t *testing.T) {
		if f := byName(findings, "System AI Component"); f != nil {
			t.Error("read Chromium's System Profile, which is not a user profile")
		}
		if f := byName(findings, "British English AI Language Pack"); f != nil {
			t.Error("reported a Firefox language pack as an extension")
		}
	})

	t.Run("the newest installed version wins", func(t *testing.T) {
		f := byName(findings, "Claude")
		if f == nil {
			t.Fatal("Claude not found")
		}
		if f.Version != "1.0.84" {
			t.Errorf("version = %q, want 1.0.84 (the stale 1.0.9 directory must lose)", f.Version)
		}
		if f.Client != "Google Chrome" {
			t.Errorf("client = %q, want Google Chrome", f.Client)
		}
		if !strings.HasSuffix(f.Source, filepath.Join("fcoeoabgfenejglbffodgkkbkcdhcgfn", "1.0.84_0")) {
			t.Errorf("source = %q, want the 1.0.84 extension directory", f.Source)
		}
		if f.Digest == "" {
			t.Error("no digest recorded")
		}
	})

	t.Run("reach is mapped from the declared permissions", func(t *testing.T) {
		f := byName(findings, "Claude")
		for _, r := range []model.Reach{
			model.ReachShell,       // nativeMessaging
			model.ReachBrowserTabs, // tabs, scripting, debugger
			model.ReachNetwork,     // <all_urls>
			model.ReachCredentials, // identity
			model.ReachFilesystem,  // downloads
		} {
			if !hasReach(f, r) {
				t.Errorf("missing reach %q, got %v", r, f.Reach)
			}
		}
	})

	t.Run("each classification signal is named in the notes", func(t *testing.T) {
		cases := []struct{ ext, signal string }{
			{"Claude", "known-id"},
			{"Monica - AI Assistant", "name-match"},
			{"Note Helper", "native-host"},
			{"Tab Utility Pro", "permission-shape"},
		}
		for _, c := range cases {
			f := byName(findings, c.ext)
			if f == nil {
				t.Errorf("%s not found", c.ext)
				continue
			}
			if !notesContain(f, "ai signal: "+c.signal) {
				t.Errorf("%s: no %q signal in notes %v", c.ext, c.signal, f.Notes)
			}
		}
	})

	t.Run("an ordinary looking extension is caught by its native messaging host", func(t *testing.T) {
		f := byName(findings, "Note Helper")
		if f == nil {
			t.Fatal("Note Helper not found")
		}
		if !notesContain(f, "com.example.ai_agent_bridge") {
			t.Errorf("host not named in notes: %v", f.Notes)
		}
		if !hasReach(f, model.ReachShell) {
			t.Errorf("native messaging did not map to shell reach: %v", f.Reach)
		}
		if len(f.Reach) == 0 || f.Catalogue != nil {
			t.Errorf("unexpected catalogue match for an unknown id: %+v", f.Catalogue)
		}
	})

	t.Run("known ids carry a catalogue match", func(t *testing.T) {
		f := byName(findings, "Claude")
		if f.Catalogue == nil || f.Catalogue.Publisher != "Anthropic" || !f.Catalogue.Verified {
			t.Errorf("catalogue match = %+v, want a verified Anthropic entry", f.Catalogue)
		}
	})

	t.Run("every profile is read, not just the default one", func(t *testing.T) {
		var profiles []string
		for _, f := range findings {
			for _, n := range f.Notes {
				if strings.HasPrefix(n, "browser profile ") {
					profiles = append(profiles, n)
				}
			}
		}
		joined := strings.Join(profiles, "|")
		for _, want := range []string{`"Default"`, `"Profile 1"`, `"default-release"`, `"work"`} {
			if !strings.Contains(joined, want) {
				t.Errorf("profile %s never read, saw %v", want, profiles)
			}
		}
	})

	t.Run("native messaging hosts are inventoried with the binary they start", func(t *testing.T) {
		host := byName(findings, "com.example.ai_agent_bridge")
		if host == nil {
			t.Fatal("native messaging host not reported")
		}
		if host.Kind != model.KindConnector {
			t.Errorf("kind = %q, want connector", host.Kind)
		}
		if host.Command != "/Applications/Example.app/Contents/Helpers/agent-host" {
			t.Errorf("command = %q, want the binary path from the host manifest", host.Command)
		}
		if !notesContain(host, "aabbccddeeffgghhiijjkkllmmnnoopp (Note Helper") {
			t.Errorf("host does not name the installed extension it serves: %v", host.Notes)
		}
		// A host with no AI signal at all is still inventoried: it is a local
		// binary a browser can start.
		if byName(findings, "com.example.printer") == nil {
			t.Error("a non-AI native messaging host was dropped")
		}
		sys := byName(findings, "com.acme.claude_bridge")
		if sys == nil {
			t.Fatal("machine-wide native messaging host not read")
		}
		if sys.Scope != model.ScopeSystem {
			t.Errorf("scope = %q, want system for a machine-wide host", sys.Scope)
		}
		if ff := byName(findings, "com.example.notes_sync"); ff == nil || ff.Client != "Firefox" {
			t.Errorf("Firefox native messaging host missing or misattributed: %+v", ff)
		}
	})

	t.Run("a malformed manifest becomes an error, not a panic", func(t *testing.T) {
		var found bool
		for _, e := range errs {
			if strings.HasSuffix(e.Path, "com.example.broken.json") {
				found = true
				if e.Scanner != "browsers" || e.Err == "" {
					t.Errorf("bad scan error: %+v", e)
				}
			}
		}
		if !found {
			t.Errorf("malformed host manifest produced no ScanError, got %v", errs)
		}
	})

	t.Run("a host registered for a browser that was never run is still reported", func(t *testing.T) {
		var vivaldi *model.Finding
		for i := range findings {
			if findings[i].Client == "Vivaldi" {
				vivaldi = &findings[i]
			}
		}
		if vivaldi == nil {
			t.Fatal("host manifest for an uninstalled browser was dropped")
		}
		var admitted bool
		for _, g := range gaps {
			if g.Area == "browsers with no profile on disk" && strings.Contains(g.Reason, "Vivaldi") {
				admitted = true
			}
		}
		if !admitted {
			t.Errorf("the run did not say Vivaldi has no profile to read: %+v", gaps)
		}
	})

	t.Run("the run states its blind spots", func(t *testing.T) {
		joined := ""
		for _, g := range gaps {
			joined += g.Area + ": " + g.Reason + "\n"
		}
		for _, want := range []string{"goes stale", "renamed", "history, cookies", "Safari"} {
			if !strings.Contains(joined, want) {
				t.Errorf("no gap mentioning %q:\n%s", want, joined)
			}
		}
		var summary string
		for _, g := range gaps {
			if g.Area == "browsers read" {
				summary = g.Reason
			}
		}
		if !strings.Contains(summary, "Google Chrome, Firefox") || !strings.Contains(summary, "4 profiles") {
			t.Errorf("coverage summary claims the wrong browsers or profiles: %q", summary)
		}
	})
}

func TestScanLinuxFixture(t *testing.T) {
	findings, _, errs := scanFixture(t, "linux", "linux", "linuxsys")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// "Companion" is an ordinary name; the publisher and the local model bridge
	// are what give it away.
	f := byName(findings, "Companion")
	if f == nil {
		t.Fatalf("Chrome extension on Linux not found, got %v", names(findings, model.KindBrowserExtension))
	}
	if f.Catalogue == nil || f.Catalogue.Name != "Perplexity - AI Companion" {
		t.Errorf("catalogue match = %+v", f.Catalogue)
	}
	if f.Catalogue.Verified {
		t.Error("an unverified catalogue entry must not claim to be verified")
	}
	if !notesContain(f, "unverified") {
		t.Errorf("unverified catalogue entry not flagged in notes: %v", f.Notes)
	}
	if brave := byName(findings, "Tab Utility Pro"); brave == nil || brave.Client != "Brave" {
		t.Errorf("Brave profile not read: %+v", brave)
	}
	if h := byName(findings, "com.example.system_agent"); h == nil || h.Scope != model.ScopeSystem {
		t.Errorf("machine-wide Linux host manifest not read: %+v", h)
	}
	if h := byName(findings, "com.example.gpt_relay"); h == nil || h.Client != "Firefox" {
		t.Errorf("Firefox host manifest on Linux not read: %+v", h)
	}
	// The Firefox add-on has an ordinary name and no AI wording; the only
	// reason it surfaces is the local model bridge that names its id.
	if f := byName(findings, "Tab Saver"); f == nil {
		t.Error("Firefox add-on attached to a local model bridge was not reported")
	} else if !notesContain(f, "ai signal: native-host") {
		t.Errorf("wrong signal for the Firefox add-on: %v", f.Notes)
	}
	if f := byName(findings, "Reader Mode"); f != nil {
		t.Errorf("a plain reader add-on was reported as AI-aware: %v", f.Notes)
	}
}

func TestScanWindowsFixture(t *testing.T) {
	findings, gaps, errs := scanFixture(t, "windows", "win", "")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	f := byName(findings, "ChatGPT")
	if f == nil {
		t.Fatalf("Chrome extension on Windows not found, got %v", names(findings, model.KindBrowserExtension))
	}
	if f.Publisher != "OpenAI" {
		t.Errorf("publisher = %q, want OpenAI", f.Publisher)
	}
	if byName(findings, "Ad Blocker Lite") != nil {
		t.Error("the Opera ad blocker was reported as AI-aware")
	}
	// Opera keeps the profile in the user data directory itself; the fixture
	// proves that layout is walked even though nothing there is reported.
	var readOpera bool
	for _, g := range gaps {
		if strings.Contains(g.Reason, "Opera") {
			readOpera = true
		}
		if g.Area == "native messaging hosts on Windows" && !strings.Contains(g.Reason, "registry") {
			t.Errorf("Windows registry gap is missing its reason: %+v", g)
		}
	}
	if !readOpera {
		t.Errorf("Opera profile layout was not walked: %+v", gaps)
	}
	var sawRegistryGap bool
	for _, g := range gaps {
		if g.Area == "native messaging hosts on Windows" {
			sawRegistryGap = true
		}
	}
	if !sawRegistryGap {
		t.Error("no gap admitting that Windows native messaging hosts live in the registry")
	}
}

func TestScanEmptyMachine(t *testing.T) {
	home := t.TempDir()
	s := scanner{sysRoot: t.TempDir()}
	findings, gaps, errs := s.Scan(model.Env{OS: "darwin", HomeDir: home})
	if len(findings) != 0 {
		t.Errorf("findings on an empty machine: %v", findings)
	}
	if len(errs) != 0 {
		t.Errorf("errors on an empty machine: %v", errs)
	}
	var sawNone bool
	for _, g := range gaps {
		if g.Area == "browsers" && strings.Contains(g.Reason, "no browser profile") {
			sawNone = true
		}
	}
	if !sawNone {
		t.Errorf("an empty machine must say so, got %v", gaps)
	}
}

// TestNewUsesRealRoot guards the production constructor, which the fixture
// tests deliberately bypass.
func TestNewUsesRealRoot(t *testing.T) {
	s, ok := New().(scanner)
	if !ok {
		t.Fatal("New did not return the package scanner")
	}
	if s.sysRoot != "/" {
		t.Errorf("sysRoot = %q, want /", s.sysRoot)
	}
	if New().Name() != "browsers" {
		t.Errorf("scanner name = %q", New().Name())
	}
}

func TestUnreadableProfileBecomesScanError(t *testing.T) {
	// See the note on the same skip in the instructions package: os.Chmod does
	// not make a directory unlistable on Windows, so this cannot be simulated
	// there without an ACL API. The assertion also matches on the text
	// "permission denied", which is the Unix wording; Windows says "Access is
	// denied." Skipped rather than made to pass on a weaker assertion.
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions are ACL-based; os.Chmod cannot make a directory unlistable")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	home := t.TempDir()
	root := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	if err := os.MkdirAll(filepath.Join(root, "Default", "Extensions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "Default", "Extensions"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "Default", "Extensions"), 0o755) })

	s := scanner{sysRoot: t.TempDir()}
	findings, _, errs := s.Scan(model.Env{OS: "darwin", HomeDir: home})
	if len(findings) != 0 {
		t.Errorf("unexpected findings: %v", findings)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Err, "permission denied") {
		t.Errorf("locked profile did not become a scan error: %+v", errs)
	}
}

func TestMalformedExtensionManifest(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default", "Extensions", "aabbccddeeffgghhiijjkkllmmnnoopp", "1.0_0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := scanner{sysRoot: t.TempDir()}
	findings, _, errs := s.Scan(model.Env{OS: "darwin", HomeDir: home})
	if len(findings) != 0 {
		t.Errorf("a broken manifest produced findings: %v", findings)
	}
	if len(errs) != 1 || !strings.HasSuffix(errs[0].Path, "manifest.json") {
		t.Errorf("broken manifest did not become a scan error: %+v", errs)
	}
}

// TestFirefoxPackedArchive covers the profile that has no extensions.json,
// where the only record of an add-on is the signed archive itself.
func TestFirefoxPackedArchive(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Library", "Application Support", "Firefox")
	profile := filepath.Join(root, "Profiles", "q1w2e3r4.default")
	if err := os.MkdirAll(filepath.Join(profile, "extensions"), 0o755); err != nil {
		t.Fatal(err)
	}
	ini := "[Profile0]\nName=default\nIsRelative=1\nPath=Profiles/q1w2e3r4.default\n"
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"manifest_version":2,"name":"Sidebar GPT","version":"3.1",
		"author":"Sidebar Labs","permissions":["tabs","nativeMessaging","<all_urls>"]}`
	writeXPI(t, filepath.Join(profile, "extensions", "sidebar@example.com.xpi"), manifest)
	// An archive with no manifest at all must be an error, not a panic.
	writeXPI(t, filepath.Join(profile, "extensions", "hollow@example.com.xpi"), "")

	s := scanner{sysRoot: t.TempDir()}
	findings, _, errs := s.Scan(model.Env{OS: "darwin", HomeDir: home})
	f := byName(findings, "Sidebar GPT")
	if f == nil {
		t.Fatalf("packed add-on not read, got %v findings %v", len(findings), names(findings, model.KindBrowserExtension))
	}
	if f.Version != "3.1" || f.Publisher != "Sidebar Labs" {
		t.Errorf("version/publisher = %q/%q", f.Version, f.Publisher)
	}
	if !hasReach(f, model.ReachShell) || !hasReach(f, model.ReachNetwork) {
		t.Errorf("reach = %v, want shell and network", f.Reach)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Err, "no manifest.json") {
		t.Errorf("empty archive did not become a scan error: %+v", errs)
	}
}

func writeXPI(t *testing.T, path, manifest string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	if manifest != "" {
		w, err := zw.Create("manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(manifest)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFirefoxProfilesIni(t *testing.T) {
	dir := t.TempDir()
	elsewhere := t.TempDir()
	ini := "[General]\nStartWithLastProfile=1\n\n[Profile0]\nName=default\nIsRelative=1\nPath=Profiles/aa.default\n\n" +
		"[Profile1]\nName=moved\nIsRelative=0\nPath=" + elsewhere + "\n"
	if err := os.WriteFile(filepath.Join(dir, "profiles.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	profiles, errs := firefoxProfiles(dir)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %+v, want 2", profiles)
	}
	if profiles[0].Dir != filepath.Join(dir, "Profiles", "aa.default") {
		t.Errorf("relative profile path = %q", profiles[0].Dir)
	}
	if profiles[1].Dir != elsewhere {
		t.Errorf("absolute profile path = %q, want %q", profiles[1].Dir, elsewhere)
	}
	if profiles, errs := firefoxProfiles(filepath.Join(dir, "nothing-here")); profiles != nil || errs != nil {
		t.Errorf("a machine without Firefox is not an error: %v %v", profiles, errs)
	}
}

// TestNeverReadsBrowsingData is the promise this tool cannot afford to break:
// no history, cookie, storage or saved-password file is opened, so nothing
// from one can reach the output.
func TestNeverReadsBrowsingData(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default")
	ext := filepath.Join(profile, "Extensions", "fcoeoabgfenejglbffodgkkbkcdhcgfn", "1.0_0")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ext, "manifest.json"),
		[]byte(`{"name":"Claude","version":"1.0","permissions":["nativeMessaging"],"host_permissions":["<all_urls>"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	const secret = "SECRET-BROWSING-DATA-canary"
	for _, name := range []string{"History", "Cookies", "Login Data", "Web Data", "Preferences", "Secure Preferences", "Local Storage"} {
		if err := os.WriteFile(filepath.Join(profile, name), []byte(secret), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := scanner{sysRoot: t.TempDir()}
	findings, gaps, errs := s.Scan(model.Env{OS: "darwin", HomeDir: home})
	if len(findings) == 0 {
		t.Fatal("fixture produced no findings, so the check below proves nothing")
	}
	out, err := json.Marshal(struct {
		F []model.Finding
		G []model.Gap
		E []model.ScanError
	}{findings, gaps, errs})
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{secret, "Login Data", "Cookies", "History", "Secure Preferences"} {
		if strings.Contains(string(out), banned) {
			t.Errorf("output references browsing data %q: %s", banned, out)
		}
	}
}

func TestDigestCoversDeclarationsOnly(t *testing.T) {
	base := extension{ID: "abc", Version: "1.0", Permissions: []string{"tabs"}, HostPermissions: []string{"<all_urls>"}}
	renamed := base
	renamed.Name = "Something Else"
	renamed.Description = "new blurb"
	if digestOf(base) != digestOf(renamed) {
		t.Error("digest changed when only the display name changed")
	}
	widened := base
	widened.Permissions = []string{"tabs", "nativeMessaging"}
	if digestOf(base) == digestOf(widened) {
		t.Error("digest did not change when the extension asked for more")
	}
	reordered := base
	reordered.HostPermissions = []string{"<all_urls>"}
	reordered.Permissions = []string{"tabs"}
	if digestOf(base) != digestOf(reordered) {
		t.Error("digest is not stable across an equivalent declaration")
	}
	hosted := base
	hosted.NativeHosts = []string{"com.example.bridge"}
	if digestOf(base) == digestOf(hosted) {
		t.Error("digest ignored a newly attached native messaging host")
	}
}

func digestOf(e extension) string { return extensionDigest(e) }
