package instructions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// maxInspectBytes bounds what is held in memory and scanned for wording.
	// The digest still covers the whole file.
	maxInspectBytes = 1 << 20
	// maxSniffBytes is how far in the null-byte test for binary files looks.
	maxSniffBytes = 8000
	// maxQuotedLine keeps a single matched line from filling the terminal. A
	// matched line is the most that is ever quoted from a file.
	maxQuotedLine = 160
	// maxNotesPerKind stops one generated file from producing a page of notes.
	maxNotesPerKind = 10
)

// inspection is everything read out of one instruction file.
type inspection struct {
	digest string
	notes  []string
	// binary is set when the file contains null bytes. Binary files are not
	// reported as instruction files at all.
	binary bool
}

// inspect hashes the whole file and describes the part of it that was read.
// Every note it produces is an observation about the text.
func inspect(p string, size int64) (inspection, error) {
	f, err := os.Open(p)
	if err != nil {
		return inspection{}, err
	}
	defer f.Close()

	h := sha256.New()
	var head bytes.Buffer
	head.Grow(int(min(size, int64(maxInspectBytes))))
	if _, err := io.Copy(h, io.TeeReader(io.LimitReader(f, maxInspectBytes), &head)); err != nil {
		return inspection{}, err
	}
	rest, err := io.Copy(h, f)
	if err != nil {
		return inspection{}, err
	}

	body := head.Bytes()
	sniff := body
	if len(sniff) > maxSniffBytes {
		sniff = sniff[:maxSniffBytes]
	}
	if bytes.IndexByte(sniff, 0) >= 0 {
		return inspection{binary: true}, nil
	}

	in := inspection{digest: hex.EncodeToString(h.Sum(nil))}
	in.notes = describe(string(body), size, rest > 0)
	return in, nil
}

// describe turns the readable part of a file into plain factual notes.
func describe(body string, size int64, truncated bool) []string {
	lines := strings.Split(body, "\n")
	count := len(lines)
	if count > 0 && lines[count-1] == "" {
		count-- // a trailing newline does not start another line
	}
	first := fmt.Sprintf("%d bytes, %d lines", size, count)
	if truncated {
		first += fmt.Sprintf(", only the first %d bytes were inspected", maxInspectBytes)
	}
	notes := []string{first}

	var imports, front, signals, hidden []string
	fenced := false
	for i, line := range lines {
		n := i + 1
		trimmed := strings.TrimSpace(line)

		if i == 0 && trimmed == "---" {
			front = append(front, frontMatterNotes(lines)...)
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		// Imports inside a code block are shown to the reader rather than
		// followed, so they are not counted.
		if !fenced {
			for _, m := range importRe.FindAllStringSubmatch(line, -1) {
				imports = append(imports, fmt.Sprintf("line %d imports another file: @%s", n, m[2]))
			}
		}
		for _, sig := range overrideSignals {
			if sig.re.MatchString(line) {
				signals = append(signals, fmt.Sprintf("line %d %s: %q", n, sig.what, quoteLine(trimmed)))
				break // one note per line is enough to find it
			}
		}
		if r, ok := invisibleRune(line); ok {
			hidden = append(hidden, fmt.Sprintf("line %d contains an invisible character (U+%04X)", n, r))
		}
	}

	notes = append(notes, front...)
	notes = append(notes, capNotes(imports, "imports")...)
	notes = append(notes, capNotes(signals, "matching lines")...)
	notes = append(notes, capNotes(hidden, "lines with invisible characters")...)
	return notes
}

// frontMatterNotes reports the header keys that decide when a rule file is fed
// to the model, because they are the difference between a file the user opts
// into and one that is always present.
func frontMatterNotes(lines []string) []string {
	var out []string
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		k, v, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch strings.ToLower(k) {
		case "alwaysapply", "applyto", "globs", "trigger":
			out = append(out, fmt.Sprintf("front matter sets %s: %s", k, quoteLine(v)))
		}
	}
	return out
}

// capNotes trims a run of notes so that one file cannot flood the report.
func capNotes(notes []string, what string) []string {
	if len(notes) <= maxNotesPerKind {
		return notes
	}
	out := append([]string{}, notes[:maxNotesPerKind]...)
	return append(out, fmt.Sprintf("and %d further %s not listed", len(notes)-maxNotesPerKind, what))
}

func quoteLine(s string) string {
	if utf8.RuneCountInString(s) <= maxQuotedLine {
		return s
	}
	r := []rune(s)
	return string(r[:maxQuotedLine]) + "…"
}

// importRe matches the @path form that Claude Code, Cursor and Windsurf all use
// to pull another file into the same context. It requires a path shape so that
// an email address or an @mention is not mistaken for an import.
var importRe = regexp.MustCompile(`(^|[\s(\[])@([~./][^\s)\]]*|[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+)`)

// overrideSignals are wordings that would suppress or override the user's own
// review of what the agent does. A match is reported with its line number and
// nothing more: this scanner states what the text says, it does not decide what
// the text is for.
var overrideSignals = []struct {
	what string
	re   *regexp.Regexp
}{
	{
		what: "names a permission-bypassing flag or mode",
		re:   regexp.MustCompile(`(?i)(--dangerously-skip-permissions|--yolo\b|\byolo mode\b|\b(?:enable|enabled|set|turn on|use|using|with|allow|always)\s+auto[- ]?approve\b|\bauto_?[aA]pprove\s*[:=]|(?-i:\bautoApprove\b)|\bbypassPermissions\b)`),
	},
	{
		what: "asks the agent to act without user confirmation",
		re:   regexp.MustCompile(`(?i)\b(?:skip|bypass|suppress|avoid|without|no need (?:for|to)|don'?t|do not|never)\b[^.\n]{0,40}\b(?:confirmation|confirming|approval|permission|permissions|sign-?off|asking the user|ask the user|ask me first|prompting the user|prompt the user|checking with the user|check with the user|confirm with)\b`),
	},
	{
		what: "asks the agent to set aside earlier instructions",
		re:   regexp.MustCompile(`(?i)\b(?:ignore|disregard|forget|override|overrule|supersede|discard)\b[^.\n]{0,30}\b(?:previous|prior|earlier|above|preceding|system|any other|all other|any previous|all previous)\b[^.\n]{0,25}\b(?:instruction|instructions|prompt|prompts|rule|rules|guidance|guideline|guidelines|context|message|messages|directive|directives)\b`),
	},
	{
		what: "asks the agent to keep something from the user",
		re:   regexp.MustCompile(`(?i)\b(?:do not|don'?t|never|avoid)\b[^.\n]{0,30}\b(?:mention|mentioning|tell|telling|inform|informing|reveal|revealing|disclose|disclosing|notify|notifying|report|surface|show)\b[^.\n]{0,30}\b(?:the user|the users|the human|the operator|your user|anyone)\b`),
	},
	{
		what: "asks the agent to act without telling the user",
		re:   regexp.MustCompile(`(?i)(\bwithout (?:telling|informing|notifying|alerting|asking) (?:the )?(?:user|human|operator)\b|\bkeep (?:this|it) (?:secret|hidden|between us|to yourself)\b|\b(?:silently|quietly)\b[^.\n]{0,25}\b(?:delete|remove|send|upload|install|modify|change|replace|commit|push|disable)\b)`),
	},
	{
		what: "asks the agent to skip review or verification",
		re:   regexp.MustCompile(`(?i)\b(?:skip|bypass|disable|turn off|do not run|don'?t run|no need to run)\b[^.\n]{0,25}\b(?:code review|the review|review|tests|test suite|testing|linting|lint|the checks|verification|validation|pre-?commit|security check|security scan)\b`),
	},
}

// invisibleRune finds a zero-width, soft-hyphen or bidirectional control
// character. Those carry text a reader of the file cannot see, so their
// presence is a fact worth stating. The line itself is never quoted back,
// because quoting it would hide the character again.
func invisibleRune(line string) (rune, bool) {
	for _, r := range line {
		switch {
		case r == '\u200b', r == '\u200c', r == '\u200d', r == '\u2060', // zero width
			r == '\ufeff', r == '\u00ad', // byte order mark, soft hyphen
			r >= '\u202a' && r <= '\u202e',         // bidirectional overrides
			r >= '\u2066' && r <= '\u2069',         // bidirectional isolates
			r >= '\U000e0000' && r <= '\U000e007f': // tag characters
			return r, true
		}
	}
	return 0, false
}
