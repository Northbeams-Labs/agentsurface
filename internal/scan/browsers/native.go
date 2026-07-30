package browsers

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// nativeHost is one native messaging host manifest: a browser extension's
// declared route to a local executable. This is the part of the browser
// surface that almost nothing inventories, and it is the part that leaves the
// sandbox, so the binary path is reported verbatim.
type nativeHost struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	// AllowedOrigins is the Chromium form, "chrome-extension://<id>/".
	AllowedOrigins []string `json:"allowed_origins"`
	// AllowedExtensions is the Firefox form, a list of add-on ids.
	AllowedExtensions []string `json:"allowed_extensions"`

	Browser  string   `json:"-"`
	Source   string   `json:"-"`
	ExtIDs   []string `json:"-"`
	SystemWi bool     `json:"-"`
}

// extensionIDs normalises both manifest dialects into plain extension ids.
func (h *nativeHost) extensionIDs() []string {
	var ids []string
	for _, o := range h.AllowedOrigins {
		o = strings.TrimSuffix(strings.TrimSpace(o), "/")
		if i := strings.LastIndex(o, "/"); i >= 0 {
			o = o[i+1:]
		}
		if o != "" {
			ids = append(ids, o)
		}
	}
	ids = append(ids, h.AllowedExtensions...)
	return sortedUnique(ids)
}

// readNativeHosts reads every host manifest in dir. A missing directory means
// the browser is not installed or has no hosts registered, which is normal.
func readNativeHosts(browser, dir string, systemWide bool) ([]nativeHost, []model.ScanError) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []model.ScanError{{Scanner: scannerName, Path: dir, Err: err.Error()}}
	}
	var (
		hosts []nativeHost
		errs  []model.ScanError
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, model.ScanError{Scanner: scannerName, Path: path, Err: err.Error()})
			continue
		}
		var h nativeHost
		if err := json.Unmarshal(raw, &h); err != nil {
			errs = append(errs, model.ScanError{Scanner: scannerName, Path: path, Err: err.Error()})
			continue
		}
		if h.Name == "" {
			h.Name = strings.TrimSuffix(e.Name(), ".json")
		}
		h.Browser = browser
		h.Source = path
		h.SystemWi = systemWide
		h.ExtIDs = h.extensionIDs()
		hosts = append(hosts, h)
	}
	return hosts, errs
}

// hostFinding reports a native messaging host as a connector in its own right.
// It is reported whether or not the extension it serves is installed, because
// a registered host is a local executable a browser can start.
func hostFinding(h nativeHost, installed map[string]extension) model.Finding {
	scope := model.ScopeUser
	if h.SystemWi {
		scope = model.ScopeSystem
	}
	notes := []string{
		"native messaging host: an installed browser extension can start this local binary and exchange messages with it",
	}
	if h.Type != "" {
		notes = append(notes, "transport declared as "+h.Type)
	}
	if len(h.ExtIDs) == 0 {
		notes = append(notes, "manifest declares no allowed extension ids")
	} else {
		var known, unknown []string
		for _, id := range h.ExtIDs {
			if e, ok := installed[id]; ok {
				known = append(known, id+" ("+e.Name+", "+e.Browser+")")
				continue
			}
			unknown = append(unknown, id)
		}
		if len(known) > 0 {
			notes = append(notes, "allowed extensions installed here: "+strings.Join(known, ", "))
		}
		if len(unknown) > 0 {
			notes = append(notes, "allowed extension ids not installed here: "+strings.Join(unknown, ", "))
		}
	}
	if agentBridgeRE.MatchString(h.Name) {
		notes = append(notes, "ai signal: native-host, host name reads as an agent bridge")
	}
	if h.Description != "" {
		notes = append(notes, "declared description: "+h.Description)
	}
	return model.Finding{
		Kind:      model.KindConnector,
		Name:      h.Name,
		Client:    h.Browser,
		Scope:     scope,
		Publisher: "",
		Source:    h.Source,
		Command:   h.Path,
		Reach:     []model.Reach{model.ReachShell},
		Digest:    digest(h.Name, h.Path, h.Type, strings.Join(h.ExtIDs, ",")),
		Notes:     notes,
	}
}
