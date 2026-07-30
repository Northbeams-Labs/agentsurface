// Package networkguard holds no code. It holds the test that proves the claim
// agentsurface is published on: that the binary cannot make a network call.
//
// The claim appears in README.md, PRIVACY.md and SECURITY.md, and a claim in a
// README is worth nothing on its own. This test is the version of it that a
// contributor trips over without knowing the CI workflow exists, because it
// runs on a plain `go test ./...`.
//
// It is deliberately not a grep over source text. Source text can be
// obfuscated, guarded by a build tag for a platform nobody runs the tests on,
// or reached indirectly through a dependency. This works on the dependency
// graph of the command, resolved by the Go toolchain, for every platform the
// project ships or intends to ship.
//
// The shell script at .github/scripts/no-network.sh applies the same rules and
// additionally inspects the symbol table of the compiled binary. This file and
// that script are meant to agree; if you change a rule, change both.
//
// This file is a test file, so nothing in it is ever linked into the released
// binary. That is why it may use os/exec while the command may not.
package networkguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commandPackage is the only package that becomes a shipped binary.
const commandPackage = "./cmd/agentsurface"

// modulePath is this module. Every non-standard dependency must be inside it.
const modulePath = "github.com/Northbeams-Labs/agentsurface"

// platforms are the operating system and architecture pairs the check covers.
// macOS and Linux are released. Windows is built by CI and not released yet,
// and is checked here so the source stays clean for the day it is.
//
// Checking every platform is the point rather than a detail: an import guarded
// by `//go:build windows` is invisible to a check that only looks at the
// machine it happens to be running on.
var platforms = []struct{ goos, goarch string }{
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"windows", "amd64"},
	{"windows", "arm64"},
}

// allowedNetPackages are the only packages under the net/ prefix that may
// appear in the dependency graph.
//
// Both are pure parsers. Neither imports net, and neither can open anything;
// confirm that for yourself with `go list -deps net/url`. They are allowed
// because a detector reasonably needs to parse a URL or an address it read out
// of a configuration file, and refusing them would push that work into
// hand-rolled string handling, which is worse.
var allowedNetPackages = map[string]bool{
	"net/url":   true,
	"net/netip": true,
}

// deniedExact are packages that must never appear.
//
// net, net/http and crypto/tls are the network itself.
//
// os/exec is here for a reason worth stating: without it, every rule in this
// file could be defeated by shelling out to curl, which no import-graph check
// would ever see. Denying it also matches a scanner rule in its own right,
// since agentsurface reads what it finds on the machine and never runs it.
var deniedExact = map[string]string{
	"net":        "opens sockets",
	"crypto/tls": "negotiates TLS, so it implies a connection",
	"os/exec":    "starts processes, which would let the tool reach the network through curl or similar",
}

// deniedPrefixes are third-party packages whose whole purpose is to make HTTP
// requests. The standard-library rule below already rejects all of them, since
// none of them is in the standard library. They are named only so that the
// failure message says what was found rather than "some third-party package".
var deniedPrefixes = []string{
	"golang.org/x/net",
	"google.golang.org/grpc",
	"github.com/go-resty/resty",
	"github.com/hashicorp/go-retryablehttp",
	"github.com/hashicorp/go-cleanhttp",
	"github.com/valyala/fasthttp",
	"github.com/imroc/req",
	"github.com/parnurzeal/gorequest",
	"github.com/gorilla/websocket",
	"github.com/coder/websocket",
	"nhooyr.io/websocket",
	"github.com/gojek/heimdall",
	"github.com/levigross/grequests",
	"github.com/aws/aws-sdk-go",
	"cloud.google.com/go",
	"github.com/Azure/azure-sdk-for-go",
}

// verdict is the reason a package is rejected, or the empty string if it is
// acceptable.
func verdict(importPath string, standard bool) string {
	if reason, ok := deniedExact[importPath]; ok {
		return "denied package: " + reason
	}

	if strings.HasPrefix(importPath, "net/") && !allowedNetPackages[importPath] {
		return "network package, and not one of the two parsers on the allowlist"
	}

	if strings.HasPrefix(importPath, "crypto/tls/") || strings.HasPrefix(importPath, "os/exec/") {
		return "subpackage of a denied package"
	}

	for _, p := range deniedPrefixes {
		if importPath == p || strings.HasPrefix(importPath, p+"/") {
			return "third-party HTTP client"
		}
	}

	if !standard && importPath != modulePath && !strings.HasPrefix(importPath, modulePath+"/") {
		return "outside the Go standard library and this module. agentsurface has no third-party dependencies, and adding one is a decision to take in an issue first"
	}

	return ""
}

// TestVerdictRules checks the rule function itself before it is trusted with
// the real dependency graph. A guard that silently stopped rejecting anything
// would otherwise pass forever.
func TestVerdictRules(t *testing.T) {
	rejected := []struct {
		path     string
		standard bool
	}{
		{"net", true},
		{"net/http", true},
		{"net/http/httptrace", true},
		{"net/rpc", true},
		{"net/smtp", true},
		{"net/textproto", true},
		{"net/mail", true},
		{"crypto/tls", true},
		{"os/exec", true},
		{"golang.org/x/net/http2", false},
		{"github.com/go-resty/resty/v2", false},
		{"google.golang.org/grpc", false},
		{"example.com/anything", false},
	}
	for _, c := range rejected {
		if verdict(c.path, c.standard) == "" {
			t.Errorf("%q should be rejected and was not. The guard is not guarding.", c.path)
		}
	}

	accepted := []struct {
		path     string
		standard bool
	}{
		{"os", true},
		{"io/fs", true},
		{"path/filepath", true},
		{"encoding/json", true},
		{"crypto/sha256", true},
		{"net/url", true},
		{"net/netip", true},
		{modulePath + "/internal/model", false},
		{modulePath + "/cmd/agentsurface", false},
	}
	for _, c := range accepted {
		if reason := verdict(c.path, c.standard); reason != "" {
			t.Errorf("%q should be accepted, was rejected: %s", c.path, reason)
		}
	}
}

// TestCommandHasNoNetworkDependency resolves the real dependency graph of the
// command, on every platform, and applies the rules to it.
func TestCommandHasNoNetworkDependency(t *testing.T) {
	goBin := goToolPath(t)
	root := moduleRoot(t, goBin)

	for _, p := range platforms {
		t.Run(p.goos+"/"+p.goarch, func(t *testing.T) {
			cmd := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}} {{.Standard}}", commandPackage)
			cmd.Dir = root
			cmd.Env = append(os.Environ(),
				"GOOS="+p.goos,
				"GOARCH="+p.goarch,
				"CGO_ENABLED=0",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go list failed for %s/%s: %v\n%s", p.goos, p.goarch, err, out)
			}

			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) < 10 {
				t.Fatalf("dependency graph has only %d entries, which means go list did not do what this test assumes:\n%s", len(lines), out)
			}

			for _, line := range lines {
				fields := strings.Fields(line)
				if len(fields) != 2 {
					t.Fatalf("cannot parse go list output line %q", line)
				}
				path, standard := fields[0], fields[1] == "true"
				if reason := verdict(path, standard); reason != "" {
					t.Errorf("%s is in the dependency graph of %s for %s/%s: %s",
						path, commandPackage, p.goos, p.goarch, reason)
				}
			}
		})
	}
}

// goToolPath finds the go command. It never skips: a guard that skips when it
// cannot run is a guard that passes when it matters.
func goToolPath(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		p := filepath.Join(goroot, "bin", "go")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("cannot find the go command, so the no-network guard cannot run")
	return ""
}

// moduleRoot walks up from the test's directory to the directory holding
// go.mod, so that go list resolves ./cmd/agentsurface correctly.
func moduleRoot(t *testing.T, goBin string) string {
	t.Helper()
	out, err := exec.Command(goBin, "env", "GOMOD").Output()
	if err == nil {
		if gomod := strings.TrimSpace(string(out)); gomod != "" && gomod != os.DevNull {
			return filepath.Dir(gomod)
		}
	}

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
			t.Fatal("cannot find the module root, so the no-network guard cannot run")
		}
		dir = parent
	}
}
