package browsers

import (
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// extension is one installed browser extension as it declares itself. Every
// field here comes from a manifest or from a profile-level extension index.
// Nothing in this struct is read from browsing data.
type extension struct {
	ID          string
	Name        string
	Version     string
	Publisher   string
	Description string
	Browser     string
	Profile     string
	// Source is the extension directory (Chromium) or archive (Firefox).
	Source string
	// Permissions are the API permissions declared in the manifest.
	Permissions []string
	// HostPermissions are the origin patterns the extension declared, whether
	// from manifest v3 host_permissions or mixed into a v2 permissions list.
	HostPermissions []string
	// ContentScriptMatches are the origin patterns of declared content
	// scripts, which grant page access without a host permission entry.
	ContentScriptMatches []string
	// NativeHosts are native messaging hosts whose manifest names this
	// extension in its allowed origins. The extension manifest never names the
	// host, so the link is only visible from the host side.
	NativeHosts []string
}

// broadHostPatterns are the origin patterns that grant access to every site.
var broadHostPatterns = map[string]bool{
	"<all_urls>":  true,
	"*://*/*":     true,
	"http://*/*":  true,
	"https://*/*": true,
	"*://*":       true,
	"file:///*":   true,
	"*":           true,
}

func hasBroadHost(patterns ...[]string) bool {
	for _, set := range patterns {
		for _, p := range set {
			if broadHostPatterns[strings.TrimSpace(p)] {
				return true
			}
		}
	}
	return false
}

// tabPermissions grant reading or driving the pages the user has open.
var tabPermissions = map[string]bool{
	"tabs": true, "activeTab": true, "scripting": true, "tabGroups": true,
	"tabCapture": true, "pageCapture": true, "webNavigation": true,
	"debugger": true, "sidePanel": true,
}

// credentialPermissions are the declared routes to cookies, auth tokens and
// saved password fields.
var credentialPermissions = map[string]bool{
	"cookies": true, "webRequestAuthProvider": true, "identity": true,
	"identity.email": true, "passwordsPrivate": true, "autofillPrivate": true,
}

var clipboardPermissions = map[string]bool{
	"clipboardRead": true, "clipboardWrite": true,
}

var filesystemPermissions = map[string]bool{
	"downloads": true, "fileSystem": true, "fileBrowserHandler": true,
	"downloads.open": true,
}

// reachOrder keeps the reported capabilities in one stable order so that two
// runs of the tool produce comparable output.
var reachOrder = []model.Reach{
	model.ReachShell,
	model.ReachFilesystem,
	model.ReachNetwork,
	model.ReachBrowserTabs,
	model.ReachClipboard,
	model.ReachCredentials,
}

// reachOf maps what the extension declared onto the shared capability
// vocabulary. These are declared capabilities, not observed behaviour: an
// extension that asks for cookies may never read one.
func reachOf(e extension) []model.Reach {
	set := map[model.Reach]bool{}
	for _, p := range e.Permissions {
		switch {
		case p == "nativeMessaging":
			// Native messaging is a pipe to a local executable, so it reaches
			// past the browser sandbox entirely.
			set[model.ReachShell] = true
		case tabPermissions[p]:
			set[model.ReachBrowserTabs] = true
		case clipboardPermissions[p]:
			set[model.ReachClipboard] = true
		case credentialPermissions[p]:
			set[model.ReachCredentials] = true
		case filesystemPermissions[p]:
			set[model.ReachFilesystem] = true
		}
	}
	if len(e.NativeHosts) > 0 {
		set[model.ReachShell] = true
	}
	// Any declared origin lets the extension fetch that origin from the
	// background page, which is network reach; a broad origin is network reach
	// over every site the user visits.
	if len(e.HostPermissions) > 0 {
		set[model.ReachNetwork] = true
	}
	if len(e.ContentScriptMatches) > 0 {
		set[model.ReachBrowserTabs] = true
	}
	out := make([]model.Reach, 0, len(set))
	for _, r := range reachOrder {
		if set[r] {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		out = append(out, model.ReachUnknown)
	}
	return out
}

// sortedUnique normalises a declared list so the digest is stable across
// manifest rewrites that only reorder entries.
func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
