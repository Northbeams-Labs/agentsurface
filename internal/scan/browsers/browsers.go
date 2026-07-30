// Package browsers inventories the AI-capable browser extensions installed on
// this machine, and the native messaging hosts that let a browser extension
// talk to a local executable.
//
// It reads three kinds of file and nothing else: extension manifests, the
// profile-level add-on index Firefox keeps, and native messaging host
// manifests. It never opens history, cookies, storage, saved passwords or any
// other browsing data, and it never hashes or prints anything from them.
//
// An extension is reported when at least one signal says it is AI-aware, and
// the finding records which signal fired, so the reader can disagree with the
// classifier rather than trust it.
package browsers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

const scannerName = "browsers"

type scanner struct {
	// sysRoot is the prefix for machine-wide paths. Production is "/"; tests
	// point it at a fixture directory so a developer's own machine cannot
	// change the result of a test.
	sysRoot string
}

// New returns the browsers scanner.
func New() model.Scanner { return scanner{sysRoot: "/"} }

func (scanner) Name() string { return scannerName }

func (s scanner) Scan(env model.Env) ([]model.Finding, []model.Gap, []model.ScanError) {
	sysRoot := s.sysRoot
	if sysRoot == "" {
		sysRoot = "/"
	}
	var (
		findings     []model.Finding
		errs         []model.ScanError
		hosts        []nativeHost
		exts         []extension
		seenHost     = map[string]bool{}
		withProfiles []string
		hostsOnly    []string
		profileCount int
	)

	for _, l := range layouts(env, sysRoot) {
		var sawHost, sawProfile bool
		for i, dir := range l.NativeHostDirs {
			// The first entry is the per-user directory, the rest are
			// machine-wide.
			hs, hErrs := readNativeHosts(l.Browser, dir, i > 0)
			errs = append(errs, hErrs...)
			for _, h := range hs {
				key := h.Browser + "\x00" + h.Source
				if seenHost[key] {
					continue
				}
				seenHost[key] = true
				hosts = append(hosts, h)
				sawHost = true
			}
		}
		for _, root := range l.Roots {
			var (
				profiles []chromeProfile
				pErrs    []model.ScanError
			)
			switch l.Family {
			case familyFirefox:
				profiles, pErrs = firefoxProfiles(root)
			default:
				var one *model.ScanError
				profiles, one = chromiumProfiles(root)
				if one != nil {
					pErrs = append(pErrs, *one)
				}
			}
			errs = append(errs, pErrs...)
			for _, p := range profiles {
				sawProfile = true
				profileCount++
				var (
					pe   []extension
					peer []model.ScanError
				)
				if l.Family == familyFirefox {
					pe, peer = readFirefoxProfile(l.Browser, p)
				} else {
					pe, peer = readChromiumProfile(l.Browser, p)
				}
				exts = append(exts, pe...)
				errs = append(errs, peer...)
			}
		}
		switch {
		case sawProfile:
			withProfiles = append(withProfiles, l.Browser)
		case sawHost:
			// A browser with registered native messaging hosts but no profile
			// is usually not installed: an installer wrote the host manifest
			// for a browser the user never ran.
			hostsOnly = append(hostsOnly, l.Browser)
		}
	}

	// Join hosts to extensions. An extension manifest never names the native
	// messaging host it talks to; only the host manifest names the extension.
	byID := map[string][]string{}
	for _, h := range hosts {
		for _, id := range h.ExtIDs {
			byID[id] = append(byID[id], h.Name)
		}
	}
	installed := map[string]extension{}
	for i := range exts {
		exts[i].NativeHosts = sortedUnique(byID[exts[i].ID])
		if _, ok := installed[exts[i].ID]; !ok {
			installed[exts[i].ID] = exts[i]
		}
	}

	for _, e := range exts {
		entry, signals := classify(e)
		if len(signals) == 0 {
			continue
		}
		findings = append(findings, extensionFinding(e, entry, signals))
	}
	for _, h := range hosts {
		findings = append(findings, hostFinding(h, installed))
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Client != findings[j].Client {
			return findings[i].Client < findings[j].Client
		}
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Name < findings[j].Name
	})

	return findings, gaps(env, coverage{
		withProfiles: withProfiles,
		hostsOnly:    hostsOnly,
		profiles:     profileCount,
		extensions:   len(exts),
	}), errs
}

// coverage is what this run actually managed to read, so the gap list can say
// so rather than implying more.
type coverage struct {
	withProfiles []string
	hostsOnly    []string
	profiles     int
	extensions   int
}

// extensionFinding turns one classified extension into a finding. Notes carry
// the observed facts; nothing here is a verdict about the extension.
func extensionFinding(e extension, entry *catalogueEntry, signals []signal) model.Finding {
	notes := []string{
		fmt.Sprintf("browser profile %q", e.Profile),
		fmt.Sprintf("extension id %s", e.ID),
	}
	if len(e.Permissions) > 0 {
		notes = append(notes, "declared permissions: "+strings.Join(e.Permissions, ", "))
	}
	if len(e.HostPermissions) > 0 {
		notes = append(notes, "declared host access: "+strings.Join(e.HostPermissions, ", "))
	}
	if len(e.ContentScriptMatches) > 0 {
		notes = append(notes, "content scripts declared for: "+strings.Join(e.ContentScriptMatches, ", "))
	}
	if hasBroadHost(e.HostPermissions, e.ContentScriptMatches) {
		notes = append(notes, "host access covers every site the user visits")
	}
	switch {
	case len(e.NativeHosts) > 0:
		notes = append(notes, "native messaging hosts naming this extension: "+strings.Join(e.NativeHosts, ", "))
	case contains(e.Permissions, "nativeMessaging"):
		notes = append(notes, "declares the nativeMessaging permission; no host manifest on this machine names this extension")
	}
	if e.Description != "" {
		notes = append(notes, "declared description: "+e.Description)
	}
	for _, s := range signals {
		notes = append(notes, s.Note)
	}
	notes = append(notes, "reported because of: "+strings.Join(signalKinds(signals), ", "))

	var cat *model.CatalogueMatch
	if entry != nil {
		cat = &model.CatalogueMatch{
			ID:        e.ID,
			Name:      entry.Name,
			Publisher: entry.Publisher,
			Verified:  entry.Verified,
		}
		if !entry.Verified {
			notes = append(notes, "shipped list entry for this id is unverified; treat the catalogue name as a hint")
		}
	}

	return model.Finding{
		Kind:      model.KindBrowserExtension,
		Name:      e.Name,
		Client:    e.Browser,
		Scope:     model.ScopeUser,
		Publisher: e.Publisher,
		Version:   e.Version,
		Source:    e.Source,
		Reach:     reachOf(e),
		Catalogue: cat,
		Digest:    extensionDigest(e),
		Notes:     notes,
	}
}

// extensionDigest hashes only the declared parts that should not change on
// their own: identity, version, what it asked for, and what local binary it
// can reach. Nothing from browser storage, cookies, history or saved
// credentials is read, let alone hashed.
func extensionDigest(e extension) string {
	return digest(
		e.ID,
		e.Version,
		strings.Join(sortedUnique(e.Permissions), ","),
		strings.Join(sortedUnique(e.HostPermissions), ","),
		strings.Join(sortedUnique(e.ContentScriptMatches), ","),
		strings.Join(sortedUnique(e.NativeHosts), ","),
	)
}

func digest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// gaps states what this scanner did not look at. A tool that hides its blind
// spots is a claim rather than an inventory.
func gaps(env model.Env, c coverage) []model.Gap {
	g := []model.Gap{
		{
			Area:   "browser extensions: shipped id list",
			Reason: "the list of known AI extension ids is a snapshot and goes stale; a new assistant, or one renamed to something ordinary, is only caught if its permissions, description or native messaging host give it away",
		},
		{
			Area:   "browser extensions: classification",
			Reason: "extensions are judged on what they declare, not on what their code does; an extension that fetches a model over a permission it already has for another reason will not be reported",
		},
		{
			Area:   "browser data",
			Reason: "history, cookies, local storage, saved passwords and profile display names were deliberately not opened, so enabled-vs-disabled state and per-profile account identity are unknown; every extension present on disk is reported whether or not it is currently switched on",
		},
		{
			Area:   "browsers not covered",
			Reason: "Safari extensions live inside signed app bundles and are not read; Tor Browser, Zen, LibreWolf, Waterfox and other forks are not enumerated",
		},
		{
			Area:   "managed policy",
			Reason: "force-installed extensions pushed by enterprise policy are read only where they landed on disk; the policy files and registry keys that installed them are not read",
		},
	}
	if env.OS == "windows" {
		g = append(g, model.Gap{
			Area:   "native messaging hosts on Windows",
			Reason: "Windows registers native messaging hosts in the registry rather than in a directory, and this build reads files only, so no host manifests were enumerated on this machine",
		})
	}
	if env.OS == "linux" {
		g = append(g, model.Gap{
			Area:   "sandboxed browser installs on Linux",
			Reason: "Flatpak and Snap keep browser profiles under ~/.var/app and ~/snap rather than ~/.config, and those locations were not scanned",
		})
	}
	if len(c.withProfiles) == 0 && len(c.hostsOnly) == 0 {
		g = append(g, model.Gap{
			Area:   "browsers",
			Reason: "no browser profile or native messaging host directory was found on this machine",
		})
		return g
	}
	if len(c.withProfiles) > 0 {
		g = append(g, model.Gap{
			Area: "browsers read",
			Reason: fmt.Sprintf("read %d extensions across %d %s of: %s",
				c.extensions, c.profiles, plural(c.profiles, "profile"), strings.Join(c.withProfiles, ", ")),
		})
	}
	if len(c.hostsOnly) > 0 {
		g = append(g, model.Gap{
			Area: "browsers with no profile on disk",
			Reason: fmt.Sprintf("native messaging hosts are registered for %s but no profile directory exists, "+
				"so no extension list was read for them; the registered hosts are still reported",
				strings.Join(c.hostsOnly, ", ")),
		})
	}
	return g
}
