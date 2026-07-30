package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// baselinePath is where the local drift baseline lives. It never leaves the
// machine and holds hashes, not contents.
func baselinePath(home string) string {
	return filepath.Join(home, ".config", "agentsurface", "baseline.json")
}

type baselineFile struct {
	Digests map[string]string `json:"digests"`
}

// applyBaseline compares this run against the previous one and returns the
// items whose declared definition changed under the user, then rewrites the
// baseline.
func applyBaseline(home string, findings []model.Finding) ([]model.DriftEntry, error) {
	path := baselinePath(home)
	prev := baselineFile{Digests: map[string]string{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &prev)
	}

	next := baselineFile{Digests: map[string]string{}}
	var drift []model.DriftEntry
	for _, f := range findings {
		if f.Digest == "" {
			continue
		}
		key := string(f.Kind) + "\x00" + f.Source + "\x00" + f.Name
		next.Digests[key] = f.Digest
		if was, ok := prev.Digests[key]; ok && was != f.Digest {
			drift = append(drift, model.DriftEntry{Name: f.Name, Kind: f.Kind, Source: f.Source, WasHash: was, NowHash: f.Digest})
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return drift, err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return drift, err
	}
	return drift, os.WriteFile(path, b, 0o600)
}
