package browsers

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// family is the on-disk shape of a browser's extension storage. The two shapes
// are unrelated: Chromium keeps unpacked extension directories per profile,
// Firefox keeps signed archives plus a profile-level index.
type family string

const (
	familyChromium family = "chromium"
	familyFirefox  family = "firefox"
)

// browserLayout is one installed-or-not browser and where it keeps things.
//
// Paths below were checked against the directories a real install creates on
// this machine (Chrome, Edge, Brave, Chromium, Vivaldi, Opera and Arc all keep
// a NativeMessagingHosts directory beside their user data) and against the
// documented locations for the platforms not available here.
type browserLayout struct {
	Browser string
	Family  family
	// Roots are user data directories (Chromium) or the directory holding
	// profiles.ini (Firefox). A root that does not exist means not installed.
	Roots []string
	// NativeHostDirs hold native messaging host manifests, user then system.
	NativeHostDirs []string
}

// appSupport is the macOS per-user application support directory.
func appSupport(home string) string {
	return filepath.Join(home, "Library", "Application Support")
}

// layouts returns every browser this scanner knows how to read on env.OS.
// sysRoot is "/" in production and a fixture directory in tests, so that
// machine-wide paths stay out of the test's way.
func layouts(env model.Env, sysRoot string) []browserLayout {
	home := env.HomeDir
	switch env.OS {
	case "darwin":
		as := appSupport(home)
		sysAS := filepath.Join(sysRoot, "Library", "Application Support")
		chromium := func(name, dir, sysNative string) browserLayout {
			return browserLayout{
				Browser:        name,
				Family:         familyChromium,
				Roots:          []string{filepath.Join(as, dir)},
				NativeHostDirs: []string{filepath.Join(as, dir, "NativeMessagingHosts"), sysNative},
			}
		}
		return []browserLayout{
			chromium("Google Chrome", filepath.Join("Google", "Chrome"), filepath.Join(sysRoot, "Library", "Google", "Chrome", "NativeMessagingHosts")),
			chromium("Google Chrome Beta", filepath.Join("Google", "Chrome Beta"), filepath.Join(sysRoot, "Library", "Google", "Chrome Beta", "NativeMessagingHosts")),
			chromium("Google Chrome Canary", filepath.Join("Google", "Chrome Canary"), filepath.Join(sysRoot, "Library", "Google", "Chrome Canary", "NativeMessagingHosts")),
			chromium("Microsoft Edge", "Microsoft Edge", filepath.Join(sysRoot, "Library", "Microsoft", "Edge", "NativeMessagingHosts")),
			chromium("Brave", filepath.Join("BraveSoftware", "Brave-Browser"), filepath.Join(sysAS, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts")),
			chromium("Chromium", "Chromium", filepath.Join(sysAS, "Chromium", "NativeMessagingHosts")),
			chromium("Vivaldi", "Vivaldi", filepath.Join(sysAS, "Vivaldi", "NativeMessagingHosts")),
			chromium("Opera", "com.operasoftware.Opera", filepath.Join(sysAS, "com.operasoftware.Opera", "NativeMessagingHosts")),
			chromium("Opera GX", "com.operasoftware.OperaGX", filepath.Join(sysAS, "com.operasoftware.OperaGX", "NativeMessagingHosts")),
			chromium("Arc", filepath.Join("Arc", "User Data"), filepath.Join(sysAS, "Arc", "User Data", "NativeMessagingHosts")),
			{
				Browser: "Firefox",
				Family:  familyFirefox,
				Roots:   []string{filepath.Join(as, "Firefox")},
				NativeHostDirs: []string{
					filepath.Join(as, "Mozilla", "NativeMessagingHosts"),
					filepath.Join(sysAS, "Mozilla", "NativeMessagingHosts"),
				},
			},
		}
	case "windows":
		local := filepath.Join(home, "AppData", "Local")
		roaming := filepath.Join(home, "AppData", "Roaming")
		// Windows registers native messaging hosts in the registry rather than
		// in a directory, so NativeHostDirs is empty here and the omission is
		// reported as a gap.
		chromium := func(name, dir string) browserLayout {
			return browserLayout{Browser: name, Family: familyChromium, Roots: []string{dir}}
		}
		return []browserLayout{
			chromium("Google Chrome", filepath.Join(local, "Google", "Chrome", "User Data")),
			chromium("Microsoft Edge", filepath.Join(local, "Microsoft", "Edge", "User Data")),
			chromium("Brave", filepath.Join(local, "BraveSoftware", "Brave-Browser", "User Data")),
			chromium("Chromium", filepath.Join(local, "Chromium", "User Data")),
			chromium("Vivaldi", filepath.Join(local, "Vivaldi", "User Data")),
			chromium("Opera", filepath.Join(roaming, "Opera Software", "Opera Stable")),
			chromium("Opera GX", filepath.Join(roaming, "Opera Software", "Opera GX Stable")),
			chromium("Arc", filepath.Join(local, "Arc", "User Data")),
			{Browser: "Firefox", Family: familyFirefox, Roots: []string{filepath.Join(roaming, "Mozilla", "Firefox")}},
		}
	default:
		// Linux and the other unixes.
		cfg := filepath.Join(home, ".config")
		chromium := func(name, dir string, sysNative ...string) browserLayout {
			return browserLayout{
				Browser:        name,
				Family:         familyChromium,
				Roots:          []string{filepath.Join(cfg, dir)},
				NativeHostDirs: append([]string{filepath.Join(cfg, dir, "NativeMessagingHosts")}, sysNative...),
			}
		}
		etc := func(p ...string) string { return filepath.Join(append([]string{sysRoot, "etc"}, p...)...) }
		return []browserLayout{
			chromium("Google Chrome", "google-chrome", etc("opt", "chrome", "native-messaging-hosts")),
			chromium("Google Chrome Beta", "google-chrome-beta", etc("opt", "chrome-beta", "native-messaging-hosts")),
			chromium("Microsoft Edge", "microsoft-edge", etc("opt", "edge", "native-messaging-hosts")),
			chromium("Brave", filepath.Join("BraveSoftware", "Brave-Browser"), etc("opt", "brave", "native-messaging-hosts"), etc("brave", "native-messaging-hosts")),
			chromium("Chromium", "chromium", etc("chromium", "native-messaging-hosts"), etc("chromium-browser", "native-messaging-hosts")),
			chromium("Vivaldi", "vivaldi", etc("vivaldi", "native-messaging-hosts")),
			chromium("Opera", "opera", etc("opera", "native-messaging-hosts")),
			{
				Browser: "Firefox",
				Family:  familyFirefox,
				Roots:   []string{filepath.Join(home, ".mozilla", "firefox")},
				NativeHostDirs: []string{
					filepath.Join(home, ".mozilla", "native-messaging-hosts"),
					filepath.Join(sysRoot, "usr", "lib", "mozilla", "native-messaging-hosts"),
					filepath.Join(sysRoot, "usr", "lib64", "mozilla", "native-messaging-hosts"),
					filepath.Join(sysRoot, "usr", "share", "mozilla", "native-messaging-hosts"),
				},
			},
		}
	}
}

// chromeProfile is one profile directory inside a Chromium user data dir.
type chromeProfile struct {
	Name string // directory name, e.g. "Default" or "Profile 2"
	Dir  string
}

// chromiumProfiles lists every profile in a user data directory that has an
// Extensions folder. Opera and some forks use the user data directory itself
// as the profile, so the root counts too. A missing root is a browser that is
// not installed, which is not an error.
func chromiumProfiles(root string) ([]chromeProfile, *model.ScanError) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, &model.ScanError{Scanner: scannerName, Path: root, Err: err.Error()}
	}
	var profiles []chromeProfile
	if isDir(filepath.Join(root, "Extensions")) {
		profiles = append(profiles, chromeProfile{Name: "Default", Dir: root})
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "System Profile" {
			continue
		}
		if !isDir(filepath.Join(root, e.Name(), "Extensions")) {
			continue
		}
		// "Default" and "Profile 1".."Profile n" are the names Chromium
		// creates, but any directory holding an Extensions folder is a
		// profile, because a profile directory can be renamed or restored.
		profiles = append(profiles, chromeProfile{Name: e.Name(), Dir: filepath.Join(root, e.Name())})
	}
	return profiles, nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
