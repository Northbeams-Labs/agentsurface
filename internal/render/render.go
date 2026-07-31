// Package render turns a completed run into something a human or a machine
// reads. Both renderers always print what the run did not look at.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

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

// textWidth is the width the readable summary wraps prose to.
//
// It is a fixed number rather than the width of the terminal, for two reasons.
// Asking the terminal its size needs a dependency or a syscall per platform,
// and this tool has neither. And the output is piped and redirected often
// enough that a run through a pager and a run into a file should produce the
// same bytes.
//
// A line can still be longer than this: a path is never broken, and neither is
// a single word. Everything that is prose is wrapped.
const textWidth = 100

// itemIndent is where the second column of an item starts: two spaces, a name
// padded to 32, one space. Every continuation line inside an item lines up
// with it, so a wrapped note reads as one block rather than as a fragment
// stranded at the left margin.
var itemIndent = strings.Repeat(" ", 2+nameWidth+1)

// nameWidth is how much of an item's name the first column holds.
const nameWidth = 32

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
			wrapped(w, fmt.Sprintf("  %-*s ", nameWidth, trunc(f.Name, nameWidth)), itemIndent, describe(f))
			// The source line is never wrapped. It is a path, it is the line
			// somebody will copy, and a path broken across two lines is a path
			// that no longer pastes.
			fmt.Fprintf(w, "%s%s\n", itemIndent, f.Source)
			for _, n := range f.Notes {
				wrapped(w, itemIndent+"note: ", itemIndent+"      ", n)
			}
		}
		fmt.Fprintln(w)
	}

	if len(r.Drift) > 0 {
		fmt.Fprintf(w, "Changed since the last run (%d)\n", len(r.Drift))
		for _, d := range r.Drift {
			fmt.Fprintf(w, "  %-*s %s\n", nameWidth, trunc(d.Name, nameWidth), d.Source)
		}
		fmt.Fprintln(w)
	}

	if len(r.Errors) > 0 {
		fmt.Fprintf(w, "Could not read (%d)\n", len(r.Errors))
		for _, e := range r.Errors {
			writeError(w, e)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "What this did not look at")
	if len(r.Gaps) == 0 {
		fmt.Fprintln(w, "  nothing recorded, which almost certainly means a scanner forgot to say")
	}
	for _, g := range r.Gaps {
		wrapped(w, "  "+g.Area+": ", "    ", g.Reason)
	}
	fmt.Fprintln(w, "\nThis tool inventories what is installed. It does not judge whether any of it is safe.")
}

// writeError writes one failure. The scanner and the path stay on one line and
// are never broken, because the path is the useful part and a broken path is
// no longer a path. The message goes after it when it fits and underneath it,
// lined up, when it does not.
func writeError(w io.Writer, e model.ScanError) {
	indent := strings.Repeat(" ", 4+len([]rune(e.Scanner)))
	if e.Path == "" {
		wrapped(w, "  "+e.Scanner+": ", indent, e.Err)
		return
	}
	head := fmt.Sprintf("  %s: %s", e.Scanner, e.Path)
	if len([]rune(head))+1+len([]rune(e.Err)) <= textWidth {
		fmt.Fprintf(w, "%s %s\n", head, e.Err)
		return
	}
	fmt.Fprintln(w, head)
	wrapped(w, indent, indent, e.Err)
}

// wrapped writes text after first, breaking it at textWidth and starting every
// line after the first with cont. Both prefixes are counted against the width,
// so the text stays inside one column however deep the prefix is.
func wrapped(w io.Writer, first, cont, text string) {
	prefix := first
	for {
		room := textWidth - len([]rune(prefix))
		if room < minRoom {
			room = minRoom
		}
		line, rest := cut(text, room)
		fmt.Fprintf(w, "%s%s\n", prefix, line)
		if rest == "" {
			return
		}
		text, prefix = rest, cont
	}
}

// minRoom is the least text a wrapped line will carry. A prefix deep enough to
// leave less than this would otherwise wrap one word per line.
const minRoom = 24

// cut splits s into a line of at most width runes and the remainder. It breaks
// at a space and nowhere else, so a path or any other unbroken run of
// characters comes back whole and over-long rather than cut in half. Only the
// space it broke at is dropped.
func cut(s string, width int) (line, rest string) {
	r := []rune(s)
	if len(r) <= width {
		return s, ""
	}
	brk := -1
	for i := width; i > 0; i-- {
		if r[i] == ' ' {
			brk = i
			break
		}
	}
	if brk <= 0 {
		// Nothing to break at inside the width. Run past it to the next space
		// rather than splitting the word.
		for i := width; i < len(r); i++ {
			if r[i] == ' ' {
				brk = i
				break
			}
		}
	}
	if brk <= 0 {
		return s, ""
	}
	line = strings.TrimRight(string(r[:brk]), " ")
	rest = strings.TrimLeft(string(r[brk:]), " ")
	return line, rest
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
