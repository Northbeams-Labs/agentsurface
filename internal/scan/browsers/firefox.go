package browsers

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// firefoxProfiles parses profiles.ini, which is the only file that says where
// a Firefox profile actually lives; the directory names are random.
func firefoxProfiles(root string) ([]chromeProfile, []model.ScanError) {
	iniPath := filepath.Join(root, "profiles.ini")
	f, err := os.Open(iniPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []model.ScanError{{Scanner: scannerName, Path: iniPath, Err: err.Error()}}
	}
	defer f.Close()

	var (
		profiles   []chromeProfile
		inProfile  bool
		name, path string
		relative   = true
	)
	flush := func() {
		if !inProfile || path == "" {
			return
		}
		dir := path
		if relative {
			dir = filepath.Join(root, filepath.FromSlash(path))
		}
		if name == "" {
			name = filepath.Base(dir)
		}
		profiles = append(profiles, chromeProfile{Name: name, Dir: dir})
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			flush()
			inProfile = strings.HasPrefix(line, "[Profile")
			name, path, relative = "", "", true
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Name":
			name = strings.TrimSpace(v)
		case "Path":
			path = strings.TrimSpace(v)
		case "IsRelative":
			relative = strings.TrimSpace(v) != "0"
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return profiles, []model.ScanError{{Scanner: scannerName, Path: iniPath, Err: err.Error()}}
	}
	return profiles, nil
}

// firefoxAddons is the profile-level index Firefox keeps of installed add-ons.
// It carries the granted permissions, which the packed archive alone does not.
type firefoxAddons struct {
	Addons []struct {
		ID            string `json:"id"`
		Version       string `json:"version"`
		Type          string `json:"type"`
		Path          string `json:"path"`
		Location      string `json:"location"`
		DefaultLocale struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Creator     string `json:"creator"`
		} `json:"defaultLocale"`
		UserPermissions *struct {
			Permissions []string `json:"permissions"`
			Origins     []string `json:"origins"`
		} `json:"userPermissions"`
		OptionalPermissions *struct {
			Permissions []string `json:"permissions"`
			Origins     []string `json:"origins"`
		} `json:"optionalPermissions"`
	} `json:"addons"`
}

// readFirefoxProfile reads one Firefox profile. extensions.json is preferred
// because it is the profile's own record of what is installed and what it was
// granted; where it is missing the packed archives are read instead.
func readFirefoxProfile(browser string, p chromeProfile) ([]extension, []model.ScanError) {
	idxPath := filepath.Join(p.Dir, "extensions.json")
	raw, err := os.ReadFile(idxPath)
	switch {
	case err == nil:
		var idx firefoxAddons
		if err := json.Unmarshal(raw, &idx); err != nil {
			errs := []model.ScanError{{Scanner: scannerName, Path: idxPath, Err: err.Error()}}
			exts, more := readFirefoxArchives(browser, p)
			return exts, append(errs, more...)
		}
		var exts []extension
		for _, a := range idx.Addons {
			if a.Type != "" && a.Type != "extension" {
				continue
			}
			ext := extension{
				ID:          a.ID,
				Name:        a.DefaultLocale.Name,
				Version:     a.Version,
				Publisher:   a.DefaultLocale.Creator,
				Description: a.DefaultLocale.Description,
				Browser:     browser,
				Profile:     p.Name,
				Source:      a.Path,
			}
			if ext.Source == "" {
				ext.Source = idxPath
			}
			if ext.Name == "" {
				ext.Name = a.ID
			}
			var perms, hosts []string
			if a.UserPermissions != nil {
				perms = append(perms, a.UserPermissions.Permissions...)
				hosts = append(hosts, a.UserPermissions.Origins...)
			}
			if a.OptionalPermissions != nil {
				perms = append(perms, a.OptionalPermissions.Permissions...)
				hosts = append(hosts, a.OptionalPermissions.Origins...)
			}
			// Firefox mixes origins into the permission list too.
			apiPerms, moreHosts := splitPermissions(toAny(perms))
			ext.Permissions = sortedUnique(apiPerms)
			ext.HostPermissions = sortedUnique(append(hosts, moreHosts...))
			exts = append(exts, ext)
		}
		return exts, nil
	case errors.Is(err, fs.ErrNotExist):
		return readFirefoxArchives(browser, p)
	default:
		return nil, []model.ScanError{{Scanner: scannerName, Path: idxPath, Err: err.Error()}}
	}
}

// readFirefoxArchives reads manifest.json out of each installed add-on, either
// from the packed .xpi (a zip) or from an unpacked directory.
func readFirefoxArchives(browser string, p chromeProfile) ([]extension, []model.ScanError) {
	dir := filepath.Join(p.Dir, "extensions")
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
		path := filepath.Join(dir, e.Name())
		var (
			ext  extension
			rerr error
		)
		switch {
		case e.IsDir():
			ext, rerr = readChromiumManifest(path)
			ext.ID = e.Name()
		case strings.HasSuffix(strings.ToLower(e.Name()), ".xpi"):
			ext, rerr = readXPIManifest(path)
			ext.ID = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		default:
			continue
		}
		if rerr != nil {
			errs = append(errs, model.ScanError{Scanner: scannerName, Path: path, Err: rerr.Error()})
			continue
		}
		ext.Browser = browser
		ext.Profile = p.Name
		ext.Source = path
		exts = append(exts, ext)
	}
	return exts, errs
}

// maxManifestBytes caps how much of an archived manifest is read, so that a
// malformed or hostile archive cannot exhaust memory.
const maxManifestBytes = 4 << 20

func readXPIManifest(path string) (extension, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return extension{}, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return extension{}, err
		}
		raw, err := io.ReadAll(io.LimitReader(rc, maxManifestBytes))
		rc.Close()
		if err != nil {
			return extension{}, err
		}
		var m chromiumManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return extension{}, err
		}
		perms, hosts := splitPermissions(m.Permissions)
		optPerms, optHosts := splitPermissions(m.OptionalPerms)
		ext := extension{
			Name:            m.Name,
			Version:         m.Version,
			Description:     m.Description,
			Publisher:       authorOf(m.Author),
			Permissions:     sortedUnique(append(perms, optPerms...)),
			HostPermissions: sortedUnique(append(append(append(hosts, optHosts...), m.HostPermissions...), m.OptionalHosts...)),
		}
		var matches []string
		for _, cs := range m.ContentScripts {
			matches = append(matches, cs.Matches...)
		}
		ext.ContentScriptMatches = sortedUnique(matches)
		// A packed add-on cannot be localised without unzipping _locales too;
		// an unresolved placeholder is left as declared rather than guessed.
		return ext, nil
	}
	return extension{}, errors.New("no manifest.json inside archive")
}

func toAny(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}
