package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

func item(name, digest string) model.Finding {
	return model.Finding{
		Kind:   model.KindMCPServer,
		Name:   name,
		Scope:  model.ScopeUser,
		Source: "/home/somebody/.config/" + name + ".json",
		Digest: digest,
	}
}

func readRaw(t *testing.T, home string) string {
	t.Helper()
	b, err := os.ReadFile(baselinePath(home))
	if err != nil {
		t.Fatalf("baseline unreadable: %v", err)
	}
	return string(b)
}

func parseBaseline(t *testing.T, home string) baselineFile {
	t.Helper()
	var f baselineFile
	if err := json.Unmarshal([]byte(readRaw(t, home)), &f); err != nil {
		t.Fatalf("baseline is not valid JSON: %v", err)
	}
	return f
}

func TestFirstRunReportsNothingAndWritesTheBaseline(t *testing.T) {
	home := t.TempDir()
	findings := []model.Finding{item("alpha", "sha256:aaa"), item("beta", "sha256:bbb")}

	drift, err := applyBaseline(home, findings)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if len(drift) != 0 {
		t.Errorf("first run reported drift: %+v", drift)
	}

	got := parseBaseline(t, home)
	if got.Version != baselineVersion {
		t.Errorf("version = %d, want %d", got.Version, baselineVersion)
	}
	if len(got.Entries) != 2 {
		t.Errorf("got %d entries, want 2", len(got.Entries))
	}
}

func TestUnchangedSecondRunReportsNothing(t *testing.T) {
	home := t.TempDir()
	findings := []model.Finding{item("alpha", "sha256:aaa"), item("beta", "sha256:bbb")}

	if _, err := applyBaseline(home, findings); err != nil {
		t.Fatal(err)
	}
	drift, err := applyBaseline(home, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Fatalf("an unchanged run reported drift: %+v", drift)
	}
}

func TestChangedDigestReportsExactlyOneEntry(t *testing.T) {
	home := t.TempDir()
	before := []model.Finding{item("alpha", "sha256:aaa"), item("beta", "sha256:bbb")}
	if _, err := applyBaseline(home, before); err != nil {
		t.Fatal(err)
	}

	after := []model.Finding{item("alpha", "sha256:CHANGED"), item("beta", "sha256:bbb")}
	drift, err := applyBaseline(home, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 {
		t.Fatalf("got %d drift entries, want 1: %+v", len(drift), drift)
	}
	d := drift[0]
	if d.Name != "alpha" || d.WasHash != "sha256:aaa" || d.NowHash != "sha256:CHANGED" {
		t.Errorf("drift entry is wrong: %+v", d)
	}
	if d.Kind != model.KindMCPServer || d.Source == "" || d.Detected == "" {
		t.Errorf("drift entry is missing identifying detail: %+v", d)
	}

	// The change is reported once, not on every run afterwards.
	again, err := applyBaseline(home, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("the same change was reported twice: %+v", again)
	}
}

func TestFirstSeenSurvivesADriftReport(t *testing.T) {
	home := t.TempDir()
	timeNow = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	defer func() { timeNow = time.Now }()

	if _, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")}); err != nil {
		t.Fatal(err)
	}
	timeNow = func() time.Time { return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:zzz")}); err != nil {
		t.Fatal(err)
	}

	for _, e := range parseBaseline(t, home).Entries {
		if !strings.HasPrefix(e.FirstSeen, "2026-01-01") {
			t.Errorf("first_seen = %q, want the original sighting", e.FirstSeen)
		}
		if !strings.HasPrefix(e.LastSeen, "2026-03-01") {
			t.Errorf("last_seen = %q, want this run", e.LastSeen)
		}
	}
}

func TestCorruptBaselineDegradesToNoDrift(t *testing.T) {
	home := t.TempDir()
	if _, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath(home), []byte("{not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}

	drift, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:DIFFERENT")})
	if err == nil {
		t.Fatal("a corrupt baseline must be reported as an error")
	}
	if len(drift) != 0 {
		t.Fatalf("a corrupt baseline produced drift: %+v", drift)
	}
	if !strings.Contains(err.Error(), "no drift is reported") {
		t.Errorf("the error does not say what it did instead: %v", err)
	}

	// The next run works from the rebuilt file.
	drift, err = applyBaseline(home, []model.Finding{item("alpha", "sha256:DIFFERENT")})
	if err != nil {
		t.Fatalf("the baseline did not recover: %v", err)
	}
	if len(drift) != 0 {
		t.Errorf("drift reported after recovery: %+v", drift)
	}
}

func TestTruncatedAndEmptyBaselinesRecover(t *testing.T) {
	for _, body := range []string{"", "{", `{"version":1,"entries":"not a map"}`, "[]", "null"} {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Dir(baselinePath(home)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(baselinePath(home), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		drift, _ := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")})
		if len(drift) != 0 {
			t.Errorf("body %q produced drift: %+v", body, drift)
		}
		if got := parseBaseline(t, home); got.Version != baselineVersion || len(got.Entries) != 1 {
			t.Errorf("body %q was not rebuilt: %+v", body, got)
		}
	}
}

func TestAnotherFormatVersionIsNotDriftAndIsNotAnError(t *testing.T) {
	// The shape this tool used before it was versioned, and a shape from some
	// future version. Neither may produce a screen of false drift, and neither
	// is a fault worth showing the user.
	// The future-format file deliberately uses the key this version would
	// compute, so that a build which forgot to check the version would report
	// drift on every entry in it.
	future := `{"version":99,"entries":{"` + baselineKey(item("alpha", "sha256:aaa")) +
		`":{"digest":"sha256:OLD","kind":"mcp_server","name":"alpha","last_seen":"` +
		time.Now().UTC().Format(time.RFC3339) + `"}}}`

	for _, body := range []string{
		"{\"digests\":{\"mcp_server|/a|alpha\":\"sha256:OLD\"}}",
		future,
	} {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Dir(baselinePath(home)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(baselinePath(home), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		drift, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")})
		if err != nil {
			t.Errorf("body %q was reported as an error: %v", body, err)
		}
		if len(drift) != 0 {
			t.Errorf("body %q produced drift on upgrade: %+v", body, drift)
		}
		if got := parseBaseline(t, home); got.Version != baselineVersion {
			t.Errorf("body %q was not rewritten at version %d", body, baselineVersion)
		}
	}
}

func TestRemovedItemIsNotDriftButIsRemembered(t *testing.T) {
	home := t.TempDir()
	both := []model.Finding{item("alpha", "sha256:aaa"), item("beta", "sha256:bbb")}
	if _, err := applyBaseline(home, both); err != nil {
		t.Fatal(err)
	}

	// beta is gone this run, for example because the tool was run from another
	// directory. That is not drift.
	drift, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")})
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Fatalf("a disappearance was reported as drift: %+v", drift)
	}
	if n := len(parseBaseline(t, home).Entries); n != 2 {
		t.Fatalf("got %d entries, want the missing item to be remembered", n)
	}

	// beta comes back changed. That is drift, because forgetting it would let a
	// change through unseen.
	drift, err = applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa"), item("beta", "sha256:CHANGED")})
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0].Name != "beta" || drift[0].WasHash != "sha256:bbb" {
		t.Fatalf("a change made while the item was absent was not reported: %+v", drift)
	}
}

func TestLongMissingItemsAreForgotten(t *testing.T) {
	home := t.TempDir()
	timeNow = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	defer func() { timeNow = time.Now }()

	if _, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa"), item("beta", "sha256:bbb")}); err != nil {
		t.Fatal(err)
	}

	timeNow = func() time.Time {
		return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(baselineRetention + time.Hour)
	}
	drift, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")})
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Errorf("forgetting an item produced drift: %+v", drift)
	}
	if n := len(parseBaseline(t, home).Entries); n != 1 {
		t.Errorf("got %d entries, want the stale one dropped", n)
	}
}

func TestBaselineHoldsHashesAndIdentifiersOnly(t *testing.T) {
	home := t.TempDir()
	f := item("alpha", "sha256:aaa")
	f.Command = "npx server --token=SECRET-TOKEN-VALUE"
	f.Notes = []string{"env sets API_KEY=SECRET-ENV-VALUE", "the file says SECRET-CONTENT"}
	f.Reach = []model.Reach{model.ReachShell}

	if _, err := applyBaseline(home, []model.Finding{f}); err != nil {
		t.Fatal(err)
	}
	raw := readRaw(t, home)
	for _, secret := range []string{"SECRET-TOKEN-VALUE", "SECRET-ENV-VALUE", "SECRET-CONTENT", "npx server", "shell"} {
		if strings.Contains(raw, secret) {
			t.Errorf("the baseline contains %q, which is not a hash or an identifier:\n%s", secret, raw)
		}
	}
	if !strings.Contains(raw, "sha256:aaa") {
		t.Errorf("the baseline is missing the digest it exists to remember:\n%s", raw)
	}
}

func TestItemsWithoutADigestAreIgnored(t *testing.T) {
	home := t.TempDir()
	drift, err := applyBaseline(home, []model.Finding{
		{Kind: model.KindInstructionFile, Name: "no digest", Source: "/tmp/x"},
		item("alpha", "sha256:aaa"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Errorf("unexpected drift: %+v", drift)
	}
	if n := len(parseBaseline(t, home).Entries); n != 1 {
		t.Errorf("got %d entries, want only the item that has something to compare", n)
	}
}

func TestNoFindingsIsSafe(t *testing.T) {
	home := t.TempDir()
	drift, err := applyBaseline(home, nil)
	if err != nil {
		t.Fatalf("an empty run failed: %v", err)
	}
	if len(drift) != 0 {
		t.Errorf("an empty run reported drift: %+v", drift)
	}
	if got := parseBaseline(t, home); got.Version != baselineVersion || len(got.Entries) != 0 {
		t.Errorf("unexpected baseline: %+v", got)
	}
}

func TestUnwritableLocationIsReportedNotFatal(t *testing.T) {
	home := t.TempDir()
	// A plain file where the config directory should be, so the write cannot
	// succeed however the run goes.
	if err := os.WriteFile(filepath.Join(home, ".config"), []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	drift, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")})
	if err == nil {
		t.Fatal("a failed write must be reported")
	}
	if len(drift) != 0 {
		t.Errorf("unexpected drift: %+v", drift)
	}
}

// TestWriteIsAtomic checks the guarantee that matters: the previous baseline is
// never the file being written into, so an interrupted run leaves the old one
// whole rather than half overwritten.
//
// It is checked with a hard link rather than os.SameFile. On Windows SameFile
// re-opens both operands by path at the moment it compares them, so two stats
// of the same path always look like the same file however many times it has
// been renamed over, and the check silently proves nothing there. A second link
// to the original file has no such problem on any platform: if the run wrote in
// place, the link sees the new bytes; if it renamed over, the link still holds
// the old ones.
func TestWriteIsAtomic(t *testing.T) {
	home := t.TempDir()
	if _, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")}); err != nil {
		t.Fatal(err)
	}
	path := baselinePath(home)
	dir := filepath.Dir(path)

	before := readRaw(t, home)
	witness := filepath.Join(dir, "witness.json")
	if err := os.Link(path, witness); err != nil {
		t.Skipf("hard links unavailable, so an in-place write cannot be told apart: %v", err)
	}

	if _, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:CHANGED")}); err != nil {
		t.Fatal(err)
	}
	if now := readRaw(t, home); now == before {
		t.Fatal("the second run did not change the baseline, so this proves nothing")
	}

	held, err := os.ReadFile(witness)
	if err != nil {
		t.Fatal(err)
	}
	if string(held) != before {
		t.Error("the baseline was written in place rather than renamed over: the previous file changed under the reader holding it")
	}
	assertNoLeftovers(t, dir)
}

func TestPermissionsAreTight(t *testing.T) {
	// Unix only, and not because it is inconvenient elsewhere. Windows has no
	// POSIX mode bits: os.Stat reports a synthesised 0666 for any writable file
	// and 0777 for any directory, and os.Chmod there can only flip the read-only
	// attribute. writeBaseline knows this and deliberately skips its tightening
	// chmod on Windows, so there is nothing here for this test to assert.
	// Locking a baseline down on Windows means an ACL, which this tool does not
	// yet set. That is a real gap, tracked in the file, not covered here.
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions are ACLs; chmod and the mode bits Stat reports are not the real thing")
	}

	home := t.TempDir()
	if _, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")}); err != nil {
		t.Fatal(err)
	}
	path := baselinePath(home)
	dir := filepath.Dir(path)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("baseline mode = %o, want 600", perm)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("baseline directory mode = %o, want 700", perm)
	}

	// A loosely created directory is tightened rather than left alone.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := applyBaseline(home, []model.Finding{item("alpha", "sha256:aaa")}); err != nil {
		t.Fatal(err)
	}
	di, err = os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("baseline directory mode = %o after a rerun, want 700", perm)
	}
}

func TestConcurrentRunsDoNotCorruptTheBaseline(t *testing.T) {
	home := t.TempDir()
	findings := []model.Finding{item("alpha", "sha256:aaa"), item("beta", "sha256:bbb"), item("gamma", "sha256:ccc")}

	var wg sync.WaitGroup
	errs := make([]error, 8)
	drifts := make([]int, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d, err := applyBaseline(home, findings)
			errs[i], drifts[i] = err, len(d)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("run %d failed: %v", i, err)
		}
		if drifts[i] != 0 {
			t.Errorf("run %d invented drift while running beside others", i)
		}
	}
	got := parseBaseline(t, home)
	if got.Version != baselineVersion || len(got.Entries) != 3 {
		t.Errorf("baseline after concurrent runs: %+v", got)
	}
	assertNoLeftovers(t, filepath.Dir(baselinePath(home)))
}

// assertNoLeftovers checks that the atomic write cleaned up after itself.
func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}
