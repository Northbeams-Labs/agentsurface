package browsers

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// chromiumManifest is the subset of a Chromium extension manifest this tool
// reads. Everything here is a declaration by the extension about itself.
type chromiumManifest struct {
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Description     string          `json:"description"`
	Author          json.RawMessage `json:"author"`
	DefaultLocale   string          `json:"default_locale"`
	Permissions     []any           `json:"permissions"`
	OptionalPerms   []any           `json:"optional_permissions"`
	HostPermissions []string        `json:"host_permissions"`
	OptionalHosts   []string        `json:"optional_host_permissions"`
	ContentScripts  []struct {
		Matches []string `json:"matches"`
	} `json:"content_scripts"`
}

// readChromiumProfile reads every extension installed in one profile.
func readChromiumProfile(browser string, p chromeProfile) ([]extension, []model.ScanError) {
	dir := filepath.Join(p.Dir, "Extensions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []model.ScanError{{Scanner: scannerName, Path: dir, Err: err.Error()}}
	}
	var (
		exts []extension
		errs []model.ScanError
	)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") || e.Name() == "Temp" {
			continue
		}
		id := e.Name()
		verDir, err := latestVersionDir(filepath.Join(dir, id))
		if err != nil {
			errs = append(errs, model.ScanError{Scanner: scannerName, Path: filepath.Join(dir, id), Err: err.Error()})
			continue
		}
		if verDir == "" {
			// An extension directory with no unpacked version inside it is
			// mid-install or mid-uninstall, not a failure.
			continue
		}
		ext, err := readChromiumManifest(verDir)
		if err != nil {
			errs = append(errs, model.ScanError{Scanner: scannerName, Path: filepath.Join(verDir, "manifest.json"), Err: err.Error()})
			continue
		}
		ext.ID = id
		ext.Browser = browser
		ext.Profile = p.Name
		exts = append(exts, ext)
	}
	return exts, errs
}

// latestVersionDir picks the highest version folder inside an extension
// directory. Chromium keeps the previous version around after an update.
func latestVersionDir(extDir string) (string, error) {
	entries, err := os.ReadDir(extDir)
	if err != nil {
		return "", err
	}
	var versions []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		if _, err := os.Stat(filepath.Join(extDir, e.Name(), "manifest.json")); err != nil {
			continue
		}
		versions = append(versions, e.Name())
	}
	if len(versions) == 0 {
		return "", nil
	}
	sort.Slice(versions, func(i, j int) bool { return lessVersion(versions[i], versions[j]) })
	return filepath.Join(extDir, versions[len(versions)-1]), nil
}

// lessVersion orders directory names like "1.0.84_0" numerically, so that
// version 10 sorts above version 9.
func lessVersion(a, b string) bool {
	as := strings.FieldsFunc(a, func(r rune) bool { return r == '.' || r == '_' })
	bs := strings.FieldsFunc(b, func(r rune) bool { return r == '.' || r == '_' })
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, aerr := strconv.Atoi(as[i])
		bi, berr := strconv.Atoi(bs[i])
		if aerr != nil || berr != nil {
			if as[i] != bs[i] {
				return as[i] < bs[i]
			}
			continue
		}
		if ai != bi {
			return ai < bi
		}
	}
	return len(as) < len(bs)
}

func readChromiumManifest(verDir string) (extension, error) {
	raw, err := os.ReadFile(filepath.Join(verDir, "manifest.json"))
	if err != nil {
		return extension{}, err
	}
	var m chromiumManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return extension{}, err
	}
	ext := extension{
		Version:   m.Version,
		Publisher: authorOf(m.Author),
		Source:    verDir,
	}
	ext.Name = resolveMessage(verDir, m.DefaultLocale, m.Name)
	ext.Description = resolveMessage(verDir, m.DefaultLocale, m.Description)

	perms, hosts := splitPermissions(m.Permissions)
	optPerms, optHosts := splitPermissions(m.OptionalPerms)
	// Optional permissions are declared but not granted until the user agrees,
	// so they are recorded as declared reach the same way; a reader who cares
	// about the difference has the manifest path.
	ext.Permissions = sortedUnique(append(perms, optPerms...))
	ext.HostPermissions = sortedUnique(append(append(append(hosts, optHosts...), m.HostPermissions...), m.OptionalHosts...))
	var matches []string
	for _, cs := range m.ContentScripts {
		matches = append(matches, cs.Matches...)
	}
	ext.ContentScriptMatches = sortedUnique(matches)
	return ext, nil
}

// splitPermissions separates API permissions from the origin patterns that a
// manifest v2 extension mixes into the same list.
func splitPermissions(in []any) (perms, hosts []string) {
	for _, v := range in {
		s, ok := v.(string)
		if !ok {
			// Manifest v2 allowed an object here (for example a match pattern
			// with a resource list); it is recorded as an opaque entry rather
			// than dropped.
			perms = append(perms, "unparsed-permission-object")
			continue
		}
		if isHostPattern(s) {
			hosts = append(hosts, s)
			continue
		}
		perms = append(perms, s)
	}
	return perms, hosts
}

func isHostPattern(s string) bool {
	if broadHostPatterns[s] {
		return true
	}
	return strings.Contains(s, "://")
}

// authorOf reads the author field, which may be a string or, in newer
// manifests, an object with an email.
func authorOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.Name != "" {
			return obj.Name
		}
		return obj.Email
	}
	return ""
}

// resolveMessage turns a "__MSG_key__" placeholder into the localised string
// from the extension's own _locales directory. Most store extensions declare
// their name this way, so without this the inventory would be a list of
// placeholders.
func resolveMessage(verDir, defaultLocale, value string) string {
	if !strings.HasPrefix(value, "__MSG_") || !strings.HasSuffix(value, "__") {
		return value
	}
	key := strings.TrimSuffix(strings.TrimPrefix(value, "__MSG_"), "__")
	locales := []string{defaultLocale, "en_US", "en"}
	for _, loc := range locales {
		if loc == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(verDir, "_locales", loc, "messages.json"))
		if err != nil {
			continue
		}
		var msgs map[string]struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(raw, &msgs); err != nil {
			continue
		}
		// Message keys are matched case-insensitively by the browser.
		for k, v := range msgs {
			if strings.EqualFold(k, key) && v.Message != "" {
				return v.Message
			}
		}
	}
	return value
}
