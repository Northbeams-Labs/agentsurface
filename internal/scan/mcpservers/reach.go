package mcpservers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// Reach is read off the declared command. It is a statement about what the
// configuration hands the server, not a claim about what the server does with
// it, and anything that cannot be read off the command stays unknown.

// shells are programs whose whole job is to run another command.
var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
	"ksh": true, "csh": true, "tcsh": true, "ash": true,
	"cmd": true, "powershell": true, "pwsh": true, "command": true,
	"env": true, "nohup": true, "xargs": true, "sudo": true, "doas": true,
}

// interpreters and package runners execute code chosen at run time, which is
// the same reach as a shell for the purposes of this inventory.
var interpreters = map[string]bool{
	"node": true, "npx": true, "npm": true, "pnpm": true, "pnpx": true,
	"yarn": true, "bun": true, "bunx": true, "deno": true,
	"python": true, "python3": true, "py": true, "pipx": true,
	"uv": true, "uvx": true, "poetry": true, "pipenv": true,
	"ruby": true, "perl": true, "php": true, "java": true, "dotnet": true,
	"go": true, "cargo": true, "docker": true, "podman": true, "nix": true,
}

// networkTransports are the declared transport types that speak to a remote
// endpoint rather than to a local process over stdio.
var networkTransports = map[string]bool{
	"http": true, "https": true, "sse": true, "streamable-http": true,
	"streamablehttp": true, "streamable_http": true, "ws": true, "wss": true,
	"websocket": true, "remote": true,
}

// credentialNames are the environment variable and header names that mean the
// configuration is handing this server a secret. Only the name is ever read.
var credentialNames = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[_-]?key|access[_-]?key|_key$|^key$|credential|auth|bearer|session|cookie|private)`)

// reachOf derives the capabilities from the declared command, args, transport
// and the names of the environment variables and headers passed in.
func reachOf(s server) []model.Reach {
	set := map[model.Reach]bool{}

	if s.url != "" || networkTransports[strings.ToLower(s.transport)] {
		set[model.ReachNetwork] = true
	}
	for _, a := range s.args {
		if strings.Contains(a, "http://") || strings.Contains(a, "https://") {
			set[model.ReachNetwork] = true
		}
	}

	base := commandBase(s.command)
	classified := false
	switch {
	case base == "":
		// nothing declared; the transport fields above are all we have
	case base == "osascript":
		set[model.ReachAppleScript] = true
		classified = true
	case shells[base]:
		set[model.ReachShell] = true
		classified = true
	case interpreters[base]:
		set[model.ReachShell] = true
		classified = true
	}
	for _, a := range s.args {
		if commandBase(a) == "osascript" {
			set[model.ReachAppleScript] = true
		}
	}

	for _, k := range append(append([]string{}, s.envKeys...), s.headerKeys...) {
		if credentialNames.MatchString(k) {
			set[model.ReachCredentials] = true
			break
		}
	}

	// A directory passed on the command line is the usual way an MCP server is
	// granted access to files. Only an argument that really is a directory on
	// this machine counts; a path that cannot be stat'ed proves nothing.
	for _, a := range s.args {
		if !filepath.IsAbs(a) {
			continue
		}
		if fi, err := os.Stat(a); err == nil && fi.IsDir() {
			set[model.ReachFilesystem] = true
			break
		}
	}

	if len(set) == 0 || (base != "" && !classified) {
		set[model.ReachUnknown] = true
	}

	out := make([]model.Reach, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// commandBase reduces "/opt/homebrew/bin/node" or "C:\\Windows\\cmd.exe" to
// the program name so the tables above can match it.
func commandBase(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	cmd = strings.ReplaceAll(cmd, `\`, "/")
	base := strings.ToLower(path.Base(cmd))
	return strings.TrimSuffix(base, ".exe")
}

// commandLine renders the declared command for a human, with anything that
// looks like a credential replaced. Environment variable values are not part of
// this string at all: they never leave the config file.
func commandLine(s server) string {
	if s.command == "" {
		if s.url == "" {
			return ""
		}
		return redactURL(s.url)
	}
	parts := make([]string, 0, len(s.args)+1)
	parts = append(parts, quoteIfNeeded(redactArg(s.command, "")))
	prev := ""
	for _, a := range s.args {
		parts = append(parts, quoteIfNeeded(redactArg(a, prev)))
		prev = a
	}
	if s.url != "" {
		parts = append(parts, redactURL(s.url))
	}
	return strings.Join(parts, " ")
}

var (
	// secretFlag matches --token, --api-key, -p and friends, so that the value
	// after them can be redacted whether it is attached with = or separate.
	secretFlag = regexp.MustCompile(`(?i)^--?[a-z0-9-]*(token|secret|password|passwd|api-?key|access-?key|auth|credential|bearer)[a-z0-9-]*$`)
	// knownSecretPrefix matches the shapes vendors publish for their keys.
	knownSecretPrefix = regexp.MustCompile(`^(sk-|sk-ant-|rk_|pk_live_|ghp_|gho_|ghu_|ghs_|ghr_|github_pat_|xox[baprs]-|glpat-|AKIA|ASIA|AIza|ya29\.|shpat_|npm_|dop_v1_|hf_|nvapi-|Bearer\s)`)
	// jwt matches a signed token pasted in as an argument.
	jwt = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*$`)
	// opaqueSecret matches a long unbroken run of key material. Paths, URLs and
	// package names are excluded so that real arguments survive.
	opaqueSecret = regexp.MustCompile(`^[A-Za-z0-9_+=-]{32,}$`)
	// sensitiveParam matches the query parameter names that carry a key.
	sensitiveParam = regexp.MustCompile(`(?i)(token|secret|password|api[_-]?key|access[_-]?key|auth|key|sig|signature)`)
)

const redacted = "[redacted]"

// redactArg replaces one argument if it looks like key material. prev is the
// argument before it, so that "--token abc123" is caught as well as
// "--token=abc123".
func redactArg(arg, prev string) string {
	if arg == "" {
		return arg
	}
	if k, v, ok := strings.Cut(arg, "="); ok && v != "" && secretFlag.MatchString(k) {
		return k + "=" + redacted
	}
	if secretFlag.MatchString(prev) && !strings.HasPrefix(arg, "-") {
		return redacted
	}
	if strings.Contains(arg, "://") {
		return redactURL(arg)
	}
	if knownSecretPrefix.MatchString(arg) || jwt.MatchString(arg) {
		return redacted
	}
	if opaqueSecret.MatchString(arg) && looksRandom(arg) {
		return redacted
	}
	return arg
}

// looksRandom is a deliberately dull test: key material mixes cases or digits
// with letters, whereas a long flag or identifier usually does not.
func looksRandom(s string) bool {
	var digits, upper, lower int
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= 'a' && r <= 'z':
			lower++
		}
	}
	return digits > 0 && (upper > 0 || lower > 0)
}

// redactURL keeps the endpoint visible, which is the fact worth reporting, and
// drops the user info and any query parameter that carries a key.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if u.User != nil {
		u.User = url.User(redacted)
	}
	if q := u.Query(); len(q) > 0 {
		changed := false
		for k := range q {
			if sensitiveParam.MatchString(k) {
				q.Set(k, redacted)
				changed = true
			}
		}
		if changed {
			u.RawQuery = q.Encode()
		}
	}
	out := u.String()
	// url.Values.Encode escapes the brackets; put them back so the marker reads
	// the same everywhere it appears.
	return strings.ReplaceAll(out, url.QueryEscape(redacted), redacted)
}

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\"") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// digestOf hashes the parts of a declaration that should not change on their
// own, so that a later run can spot a server that was quietly redefined.
//
// Environment variable values are excluded on purpose. They routinely hold API
// keys, and a hash of a secret is still a fact about the secret.
func digestOf(s server) string {
	h := sha256.New()
	write := func(field, value string) {
		fmt.Fprintf(h, "%s\x00%s\n", field, value)
	}
	write("name", s.name)
	write("command", s.command)
	for _, a := range s.args {
		write("arg", a)
	}
	for _, k := range s.envKeys {
		write("env", k) // key only, never the value
	}
	write("url", s.url)
	write("transport", s.transport)
	for _, k := range s.headerKeys {
		write("header", k)
	}
	for _, t := range s.toolNames {
		write("tool", t)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
