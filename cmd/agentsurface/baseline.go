package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// The baseline is how this tool notices a rug pull: something the user already
// approved, quietly redefined later. It only works if it is boring, so the
// rules below are deliberate.
//
//   - It is versioned. An older or newer format is treated as no baseline at
//     all, because an upgrade that printed a screen of drift would train the
//     user to ignore drift.
//   - A file it cannot read is a reported error, not a fresh set of findings.
//     Nothing here reports drift on a guess.
//   - It is written to a temporary file and renamed, so a run that is
//     interrupted halfway leaves the previous baseline intact.
//   - It holds hashes and identifiers. No file contents, no command lines and
//     nothing out of an env block ever reaches this file.
const baselineVersion = 1

// baselineRetention is how long an item that has stopped appearing is
// remembered. Long enough to survive a laptop being used on other projects for
// months, short enough that the file cannot grow forever.
const baselineRetention = 180 * 24 * time.Hour

// timeNow is a variable so tests can age a baseline without sleeping.
var timeNow = time.Now

// baselinePath is where the local drift baseline lives. It never leaves the
// machine and holds hashes, not contents.
func baselinePath(home string) string {
	return filepath.Join(home, ".config", "agentsurface", "baseline.json")
}

// baselineEntry is one remembered item. Every field here is either a hash or an
// identifier that was already printed on screen.
type baselineEntry struct {
	Digest    string     `json:"digest"`
	Kind      model.Kind `json:"kind"`
	Name      string     `json:"name"`
	Source    string     `json:"source"`
	FirstSeen string     `json:"first_seen"`
	LastSeen  string     `json:"last_seen"`
}

type baselineFile struct {
	Version int                      `json:"version"`
	Entries map[string]baselineEntry `json:"entries"`
}

// applyBaseline compares this run against the previous one, returns the items
// whose declared definition changed under the user, and rewrites the baseline.
//
// A missing item is not drift. Two reasons. The inventory covers the current
// working directory, so running the tool from somewhere else makes every
// project item vanish, and a removal report would be mostly false alarms the
// user cannot act on. And an item that is gone cannot do anything, which is the
// opposite of the case this is here to catch. The entry is kept in the file
// instead of being dropped, so that an item which disappears and comes back
// with a different digest is still reported as drift rather than being
// laundered into a first sighting.
func applyBaseline(home string, findings []model.Finding) ([]model.DriftEntry, error) {
	path := baselinePath(home)
	prev, readErr := readBaseline(path)

	now := timeNow().UTC()
	stamp := now.Format(time.RFC3339)
	next := baselineFile{Version: baselineVersion, Entries: map[string]baselineEntry{}}

	var drift []model.DriftEntry
	for _, f := range findings {
		// An item with no digest has nothing stable to compare, so it is not
		// remembered at all rather than being remembered as empty.
		if f.Digest == "" {
			continue
		}
		key := baselineKey(f)
		if _, already := next.Entries[key]; already {
			continue
		}
		entry := baselineEntry{
			Digest:    f.Digest,
			Kind:      f.Kind,
			Name:      f.Name,
			Source:    f.Source,
			FirstSeen: stamp,
			LastSeen:  stamp,
		}
		if was, seen := prev.Entries[key]; seen {
			if was.FirstSeen != "" {
				entry.FirstSeen = was.FirstSeen
			}
			if was.Digest != f.Digest {
				drift = append(drift, model.DriftEntry{
					Name:     f.Name,
					Kind:     f.Kind,
					Source:   f.Source,
					WasHash:  was.Digest,
					NowHash:  f.Digest,
					Detected: stamp,
				})
			}
		}
		next.Entries[key] = entry
	}

	// Carry forward everything that was not seen this run, until it goes stale.
	for key, was := range prev.Entries {
		if _, seen := next.Entries[key]; seen {
			continue
		}
		if stale(was.LastSeen, now) {
			continue
		}
		next.Entries[key] = was
	}

	sort.Slice(drift, func(i, j int) bool {
		if drift[i].Source != drift[j].Source {
			return drift[i].Source < drift[j].Source
		}
		return drift[i].Name < drift[j].Name
	})

	return drift, errors.Join(readErr, writeBaseline(path, next))
}

// readBaseline returns the previous run's baseline. Anything it cannot make
// sense of comes back as an empty baseline plus an error: no drift is reported
// from a file that could not be read, and the caller turns the error into a
// visible ScanError rather than a silent one.
func readBaseline(path string) (baselineFile, error) {
	empty := baselineFile{Version: baselineVersion, Entries: map[string]baselineEntry{}}

	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil // first run on this machine
	}
	if err != nil {
		return empty, fmt.Errorf("baseline %s could not be read, so no drift is reported for this run: %w", path, err)
	}

	var f baselineFile
	if err := json.Unmarshal(b, &f); err != nil {
		return empty, fmt.Errorf("baseline %s is not readable JSON and has been rebuilt, so no drift is reported for this run: %w", path, err)
	}
	// A file from another version of this tool is not a fault, so it is not
	// reported as one. It is simply not comparable, and this run becomes the
	// new first run.
	if f.Version != baselineVersion || f.Entries == nil {
		return empty, nil
	}
	return f, nil
}

// writeBaseline replaces the baseline atomically: a temporary file in the same
// directory, then a rename. An interrupted or concurrent run can therefore
// leave a stale baseline behind, but never a half written one.
func writeBaseline(path string, f baselineFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// MkdirAll leaves an existing directory's permissions alone, so tighten a
	// directory that someone else created loosely.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(dir, "baseline-*.json.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	// Harmless once the rename below has happened, and the only cleanup that
	// matters when anything before it fails.
	defer os.Remove(name)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return renameOver(name, path)
}

// renameOver replaces path with tmp, retrying briefly while the destination is
// locked.
//
// Unix renames over an open file without complaint and returns on the first
// attempt. Windows does not: replacing a file there fails with "Access is
// denied." while any other handle to it is open, and Go opens files for reading
// without FILE_SHARE_DELETE. Two runs at once are enough to hit it, because one
// is reading the baseline in readBaseline while the other is trying to replace
// it, and the loser reported a failure the user could do nothing about and lost
// its baseline write. The lock lasts as long as one small read, so waiting it
// out is the whole fix.
//
// Only a permission error is retried. Anything else is a real failure and comes
// straight back.
func renameOver(tmp, path string) error {
	const (
		attempts = 60
		pause    = 50 * time.Millisecond
	)
	for i := 0; ; i++ {
		err := os.Rename(tmp, path)
		if err == nil || i == attempts-1 || !errors.Is(err, fs.ErrPermission) {
			return err
		}
		time.Sleep(pause)
	}
}

// baselineKey identifies one item across runs. The parts are hashed together so
// that a name or a path containing the separator cannot be made to collide with
// another item.
func baselineKey(f model.Finding) string {
	sum := sha256.Sum256([]byte(string(f.Kind) + "\x00" + f.Source + "\x00" + f.Name))
	return hex.EncodeToString(sum[:])
}

// stale reports whether an item has been missing for longer than the retention
// window. An entry with an unreadable timestamp is treated as stale, so a
// damaged field cannot pin an item in the file forever.
func stale(lastSeen string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return true
	}
	return now.Sub(t) > baselineRetention
}
