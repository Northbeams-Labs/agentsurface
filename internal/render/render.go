// Package render turns a completed run into something a human or a machine
// reads. Both renderers always print what the run did not look at.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// JSON writes the whole result, including gaps and errors, as indented JSON.
func JSON(w io.Writer, r model.Result) error {
	if r.Findings == nil {
		r.Findings = []model.Finding{}
	}
	if r.Gaps == nil {
		r.Gaps = []model.Gap{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Text writes a readable summary. It states counts, then the items grouped by
// kind, then drift, then the blind spots.
func Text(w io.Writer, r model.Result) {
	byKind := map[model.Kind][]model.Finding{}
	for _, f := range r.Findings {
		byKind[f.Kind] = append(byKind[f.Kind], f)
	}

	fmt.Fprintf(w, "agentsurface %s  %s\n", r.Version, r.OS)
	fmt.Fprintf(w, "%d items found across %d categories\n\n", len(r.Findings), len(byKind))

	kinds := make([]model.Kind, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })

	for _, k := range kinds {
		items := byKind[k]
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		fmt.Fprintf(w, "%s (%d)\n", label(k), len(items))
		for _, f := range items {
			fmt.Fprintf(w, "  %-32s %s\n", trunc(f.Name, 32), describe(f))
			fmt.Fprintf(w, "  %-32s %s\n", "", f.Source)
			for _, n := range f.Notes {
				fmt.Fprintf(w, "  %-32s note: %s\n", "", n)
			}
		}
		fmt.Fprintln(w)
	}

	if len(r.Drift) > 0 {
		fmt.Fprintf(w, "Changed since the last run (%d)\n", len(r.Drift))
		for _, d := range r.Drift {
			fmt.Fprintf(w, "  %-32s %s\n", trunc(d.Name, 32), d.Source)
		}
		fmt.Fprintln(w)
	}

	if len(r.Errors) > 0 {
		fmt.Fprintf(w, "Could not read (%d)\n", len(r.Errors))
		for _, e := range r.Errors {
			fmt.Fprintf(w, "  %s: %s %s\n", e.Scanner, e.Path, e.Err)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "What this did not look at")
	if len(r.Gaps) == 0 {
		fmt.Fprintln(w, "  nothing recorded, which almost certainly means a scanner forgot to say")
	}
	for _, g := range r.Gaps {
		fmt.Fprintf(w, "  %s: %s\n", g.Area, g.Reason)
	}
	fmt.Fprintln(w, "\nThis tool inventories what is installed. It does not judge whether any of it is safe.")
}

func describe(f model.Finding) string {
	s := ""
	if f.Client != "" {
		s = f.Client
	}
	if f.Publisher != "" {
		if s != "" {
			s += ", "
		}
		s += f.Publisher
	}
	if len(f.Reach) > 0 {
		if s != "" {
			s += ", "
		}
		s += "can reach: "
		for i, r := range f.Reach {
			if i > 0 {
				s += " "
			}
			s += string(r)
		}
	}
	if f.Catalogue == nil {
		if s != "" {
			s += ", "
		}
		s += "not in catalogue"
	}
	return s
}

func label(k model.Kind) string {
	switch k {
	case model.KindMCPServer:
		return "Model context protocol servers"
	case model.KindExtension:
		return "Desktop extensions"
	case model.KindPlugin:
		return "Plugins"
	case model.KindSkill:
		return "Skills"
	case model.KindConnector:
		return "Connectors"
	case model.KindInstructionFile:
		return "Instruction files"
	case model.KindBrowserExtension:
		return "AI browser extensions"
	case model.KindScheduledTask:
		return "Scheduled agent tasks"
	}
	return string(k)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
