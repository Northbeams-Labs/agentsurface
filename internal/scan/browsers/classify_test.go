package browsers

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

func TestClassifySignals(t *testing.T) {
	cases := []struct {
		name string
		ext  extension
		want []string // signal kinds, sorted
	}{
		{
			name: "known id alone is enough",
			ext:  extension{ID: "kbfnbcaeplbcioakkpcpgfkobkghlhen", Name: "Writing Helper"},
			want: []string{"known-id"},
		},
		{
			name: "ai in the name",
			ext:  extension{ID: "unknown", Name: "Sider AI Sidebar"},
			want: []string{"name-match"},
		},
		{
			name: "ai only in the declared description",
			ext:  extension{ID: "unknown", Name: "Helper", Description: "A chatbot for your browser."},
			want: []string{"name-match"},
		},
		{
			name: "native messaging host that reads as an agent bridge",
			ext:  extension{ID: "unknown", Name: "Helper", NativeHosts: []string{"com.vendor.claude_bridge"}},
			want: []string{"native-host"},
		},
		{
			name: "a plain native messaging host is not a signal on its own",
			ext:  extension{ID: "unknown", Name: "Printer Helper", NativeHosts: []string{"com.vendor.label_printer"}},
			want: nil,
		},
		{
			name: "assistant shaped permissions with an ordinary name",
			ext: extension{
				ID: "unknown", Name: "Window Tidy",
				Permissions:     []string{"scripting", "sidePanel"},
				HostPermissions: []string{"<all_urls>"},
			},
			want: []string{"permission-shape"},
		},
		{
			name: "broad access without a channel out of the tab is not a signal",
			ext: extension{
				ID: "unknown", Name: "Password Vault",
				Permissions:     []string{"scripting", "tabs", "cookies", "offscreen"},
				HostPermissions: []string{"https://*/*", "http://*/*"},
			},
			want: nil,
		},
		{
			name: "narrow host access with a side panel is not a signal",
			ext: extension{
				ID: "unknown", Name: "Shop Helper",
				Permissions:     []string{"scripting", "sidePanel"},
				HostPermissions: []string{"https://shop.example.com/*"},
			},
			want: nil,
		},
		{
			name: "a word that merely contains ai does not match",
			ext:  extension{ID: "unknown", Name: "Maintainer Tools", Description: "Chairs, stairs and repairs."},
			want: nil,
		},
		{
			name: "several signals are all recorded",
			ext: extension{
				ID: "fcoeoabgfenejglbffodgkkbkcdhcgfn", Name: "Claude",
				Permissions:     []string{"scripting", "nativeMessaging", "sidePanel"},
				HostPermissions: []string{"<all_urls>"},
				NativeHosts:     []string{"com.anthropic.claude_browser_extension"},
			},
			want: []string{"known-id", "name-match", "native-host", "permission-shape"},
		},
		{
			name: "an empty extension is not a crash and not a finding",
			ext:  extension{},
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, signals := classify(c.ext)
			got := signalKinds(signals)
			sort.Strings(got)
			sort.Strings(c.want)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("signals = %v, want %v", got, c.want)
			}
			for _, s := range signals {
				if s.Note == "" {
					t.Error("a signal fired with no note explaining it")
				}
			}
		})
	}
}

func TestReachOf(t *testing.T) {
	cases := []struct {
		name string
		ext  extension
		want []model.Reach
	}{
		{
			name: "native messaging is shell reach",
			ext:  extension{Permissions: []string{"nativeMessaging"}},
			want: []model.Reach{model.ReachShell},
		},
		{
			name: "a host manifest naming the extension is shell reach too",
			ext:  extension{NativeHosts: []string{"com.vendor.bridge"}},
			want: []model.Reach{model.ReachShell},
		},
		{
			name: "tabs and scripting are browser tab reach",
			ext:  extension{Permissions: []string{"tabs", "scripting"}},
			want: []model.Reach{model.ReachBrowserTabs},
		},
		{
			name: "a content script is browser tab reach without a permission",
			ext:  extension{ContentScriptMatches: []string{"https://example.com/*"}},
			want: []model.Reach{model.ReachBrowserTabs},
		},
		{
			name: "clipboard permissions are clipboard reach",
			ext:  extension{Permissions: []string{"clipboardRead"}},
			want: []model.Reach{model.ReachClipboard},
		},
		{
			name: "cookies and saved credentials are credential reach",
			ext:  extension{Permissions: []string{"cookies", "identity"}},
			want: []model.Reach{model.ReachCredentials},
		},
		{
			name: "host access is network reach",
			ext:  extension{HostPermissions: []string{"https://api.example.com/*"}},
			want: []model.Reach{model.ReachNetwork},
		},
		{
			name: "downloads are filesystem reach",
			ext:  extension{Permissions: []string{"downloads"}},
			want: []model.Reach{model.ReachFilesystem},
		},
		{
			name: "an extension that declares nothing has unknown reach",
			ext:  extension{Permissions: []string{"storage", "alarms"}},
			want: []model.Reach{model.ReachUnknown},
		},
		{
			name: "reach is reported in a stable order",
			ext: extension{
				Permissions:     []string{"cookies", "clipboardWrite", "tabs", "downloads", "nativeMessaging"},
				HostPermissions: []string{"<all_urls>"},
			},
			want: []model.Reach{
				model.ReachShell, model.ReachFilesystem, model.ReachNetwork,
				model.ReachBrowserTabs, model.ReachClipboard, model.ReachCredentials,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reachOf(c.ext); !reflect.DeepEqual(got, c.want) {
				t.Errorf("reach = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSplitPermissions(t *testing.T) {
	perms, hosts := splitPermissions([]any{
		"tabs", "https://example.com/*", "<all_urls>", "*://*/*", 42,
		map[string]any{"unexpected": true},
	})
	if !reflect.DeepEqual(perms, []string{"tabs", "unparsed-permission-object", "unparsed-permission-object"}) {
		t.Errorf("permissions = %v", perms)
	}
	if !reflect.DeepEqual(hosts, []string{"https://example.com/*", "<all_urls>", "*://*/*"}) {
		t.Errorf("hosts = %v", hosts)
	}
	if p, h := splitPermissions(nil); p != nil || h != nil {
		t.Errorf("nil permissions produced %v %v", p, h)
	}
}

func TestLessVersion(t *testing.T) {
	got := []string{"1.0.9_0", "1.0.84_0", "10.0_0", "2.1_0", "not-a-version_0"}
	sort.Slice(got, func(i, j int) bool { return lessVersion(got[i], got[j]) })
	want := []string{"1.0.9_0", "1.0.84_0", "2.1_0", "10.0_0", "not-a-version_0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sorted = %v, want %v", got, want)
	}
}

func TestResolveMessage(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "_locales", "de"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_locales", "de", "messages.json"),
		[]byte(`{"ExtName":{"message":"Der Assistent"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveMessage(dir, "de", "__MSG_extName__"); got != "Der Assistent" {
		t.Errorf("resolved = %q, want the localised name (keys match case-insensitively)", got)
	}
	if got := resolveMessage(dir, "de", "Plain Name"); got != "Plain Name" {
		t.Errorf("a plain name was rewritten to %q", got)
	}
	if got := resolveMessage(dir, "fr", "__MSG_missing__"); got != "__MSG_missing__" {
		t.Errorf("an unresolvable placeholder became %q; it must stay as declared", got)
	}
}

func TestAuthorOf(t *testing.T) {
	cases := map[string]string{
		`"LastPass"`:                       "LastPass",
		`{"email":"team@example.com"}`:     "team@example.com",
		`{"name":"Acme","email":"a@b.co"}`: "Acme",
		`123`:                              "",
		``:                                 "",
	}
	for raw, want := range cases {
		if got := authorOf([]byte(raw)); got != want {
			t.Errorf("authorOf(%s) = %q, want %q", raw, got, want)
		}
	}
}

func TestExtensionIDsFromHostManifest(t *testing.T) {
	h := nativeHost{
		AllowedOrigins:    []string{"chrome-extension://abc/", "chrome-extension://def", "  ", "bad"},
		AllowedExtensions: []string{"addon@example.com"},
	}
	got := h.extensionIDs()
	want := []string{"abc", "addon@example.com", "bad", "def"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}
