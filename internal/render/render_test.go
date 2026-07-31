package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// longNote is the shape that broke the layout before this: a note longer than
// a terminal is wide, which used to run off the end and come back at column
// zero, leaving a word of it stranded under the left margin.
const longNote = "native messaging host: an installed browser extension can start this local binary and exchange messages with it"

func render(t *testing.T, r model.Result) []string {
	t.Helper()
	var buf bytes.Buffer
	Text(&buf, r)
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

func oneFinding(f model.Finding) model.Result {
	return model.Result{Tool: "agentsurface", Version: "v0.1.0", OS: "darwin", Findings: []model.Finding{f}}
}

func TestLongNoteWrapsWithTheContinuationAligned(t *testing.T) {
	lines := render(t, oneFinding(model.Finding{
		Kind:   model.KindConnector,
		Name:   "com.example.agent",
		Client: "Google Chrome",
		Source: "/tmp/fixture/host.json",
		Notes:  []string{longNote},
	}))

	var noteLines []string
	for _, l := range lines {
		if strings.Contains(l, "note: ") || strings.Contains(l, "exchange messages") {
			noteLines = append(noteLines, l)
		}
	}
	if len(noteLines) != 2 {
		t.Fatalf("expected the note to wrap onto a second line, got %d line(s):\n%s", len(noteLines), strings.Join(noteLines, "\n"))
	}

	// The continuation lines up under the note's text, not under the margin.
	want := itemIndent + "      "
	if !strings.HasPrefix(noteLines[1], want) {
		t.Errorf("continuation is not aligned under the note text.\n got %q\nwant it to start with %d spaces", noteLines[1], len(want))
	}
	if strings.HasPrefix(noteLines[1], want+" ") {
		t.Errorf("continuation is indented past the note text: %q", noteLines[1])
	}

	// Nothing is lost or invented in the wrapping.
	got := strings.TrimSpace(strings.TrimPrefix(noteLines[0], itemIndent+"note: ")) + " " +
		strings.TrimSpace(noteLines[1])
	if got != longNote {
		t.Errorf("the wrapped note does not read back as the original\n got %q\nwant %q", got, longNote)
	}

	for _, l := range noteLines {
		if len([]rune(l)) > textWidth {
			t.Errorf("wrapped line is %d runes wide, wider than %d: %q", len([]rune(l)), textWidth, l)
		}
	}
}

func TestLongDescriptionAndGapWrapToTheSameWidth(t *testing.T) {
	r := oneFinding(model.Finding{
		Kind:      model.KindBrowserExtension,
		Name:      "Example Assistant",
		Client:    "Google Chrome",
		Publisher: "An unusually long publisher name that pushes the first line of an item past the width",
		Source:    "/tmp/fixture/manifest.json",
	})
	r.Gaps = []model.Gap{{
		Area:   "browser extensions",
		Reason: "the list of known extension ids is a snapshot and goes stale; a new assistant, or one renamed to something ordinary, is only caught if its permissions or its native messaging host give it away",
	}}

	for _, l := range render(t, r) {
		if strings.HasPrefix(l, "/tmp/fixture") || strings.Contains(l, "  /tmp/fixture") {
			continue // paths are deliberately never broken
		}
		if len([]rune(l)) > textWidth {
			t.Errorf("line is %d runes wide, wider than %d: %q", len([]rune(l)), textWidth, l)
		}
	}
}

func TestPathsAreNeverBroken(t *testing.T) {
	path := "/tmp/fixture/Library/Application Support/Some Client/Extensions/abcdefghijklmnopqrstuvwxyzabcdef/1.4.2_0/manifest.json"
	lines := render(t, oneFinding(model.Finding{
		Kind:   model.KindBrowserExtension,
		Name:   "Example Assistant",
		Source: path,
	}))
	found := false
	for _, l := range lines {
		if strings.TrimSpace(l) == path {
			found = true
		}
	}
	if !found {
		t.Errorf("the source path was not printed whole on one line:\n%s", strings.Join(lines, "\n"))
	}
}

func TestLongErrorKeepsItsPathWholeAndWrapsTheMessageUnderIt(t *testing.T) {
	path := "/tmp/fixture/Library/Application Support/Mozilla/NativeMessagingHosts"
	r := model.Result{Tool: "agentsurface", Version: "v0.1.0", OS: "darwin"}
	r.Errors = []model.ScanError{{
		Scanner: "browsers",
		Path:    path,
		Err:     "open " + path + ": operation not permitted, and this sentence is here to push the line past the width",
	}}
	lines := render(t, r)

	var head, cont string
	for i, l := range lines {
		if strings.HasPrefix(l, "  browsers: ") {
			head = l
			if i+1 < len(lines) {
				cont = lines[i+1]
			}
		}
	}
	if head != "  browsers: "+path {
		t.Fatalf("the path was not left whole on its own line: %q", head)
	}
	want := strings.Repeat(" ", 4+len("browsers"))
	if !strings.HasPrefix(cont, want) || strings.HasPrefix(cont, want+" ") {
		t.Errorf("the message is not lined up under the path: %q", cont)
	}
	if len([]rune(cont)) > textWidth {
		t.Errorf("the message line is %d runes wide, wider than %d: %q", len([]rune(cont)), textWidth, cont)
	}
}

func TestShortErrorStaysOnOneLine(t *testing.T) {
	r := model.Result{Tool: "agentsurface", Version: "v0.1.0", OS: "darwin"}
	r.Errors = []model.ScanError{{Scanner: "browsers", Path: "/tmp/fixture/Profiles", Err: "permission denied"}}
	want := "  browsers: /tmp/fixture/Profiles permission denied"
	for _, l := range render(t, r) {
		if l == want {
			return
		}
	}
	t.Errorf("expected the short error on one line as %q", want)
}

func TestJSONIsUntouchedByWrapping(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, oneFinding(model.Finding{
		Kind:   model.KindConnector,
		Name:   "com.example.agent",
		Source: "/tmp/fixture/host.json",
		Notes:  []string{longNote},
	})); err != nil {
		t.Fatal(err)
	}

	var back model.Result
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Findings) != 1 || len(back.Findings[0].Notes) != 1 {
		t.Fatalf("expected one finding with one note, got %+v", back.Findings)
	}
	if back.Findings[0].Notes[0] != longNote {
		t.Errorf("the note came back changed\n got %q\nwant %q", back.Findings[0].Notes[0], longNote)
	}
}

func TestCutBreaksOnlyAtSpaces(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		width      int
		line, rest string
	}{
		{"short enough is left alone", "one two", 20, "one two", ""},
		{"breaks at the last space inside the width", "aaa bbb ccc ddd", 8, "aaa bbb", "ccc ddd"},
		{"a word longer than the width is not cut", "aaaaaaaaaaaa bb", 4, "aaaaaaaaaaaa", "bb"},
		{"a run with no space at all comes back whole", "/a/very/long/path/with/no/spaces", 8, "/a/very/long/path/with/no/spaces", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line, rest := cut(c.in, c.width)
			if line != c.line || rest != c.rest {
				t.Errorf("cut(%q, %d) = %q, %q; want %q, %q", c.in, c.width, line, rest, c.line, c.rest)
			}
		})
	}
}
