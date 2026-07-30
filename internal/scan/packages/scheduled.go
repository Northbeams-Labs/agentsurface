package packages

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// Something that starts an agent on a timer or at login is the one piece of
// this inventory that runs when nobody is watching. It is also the easiest to
// over-report, so the match here is deliberately narrow: the declared program
// has to be a program this tool recognises as an agent. A job that reaches an
// agent through a wrapper script is missed, and blindSpots says so, because an
// admitted blind spot is worth more than a list padded with every cron line on
// the machine.

// clientForBinary names the client a scheduled program belongs to, so a finding
// reads as "Claude Code" rather than as a file path.
var clientForBinary = map[string]string{
	"claude":       clientClaudeCode,
	"claude-code":  clientClaudeCode,
	"codex":        "Codex CLI",
	"gemini":       clientGeminiCLI,
	"cursor-agent": "Cursor",
	"aider":        "Aider",
	"goose":        "Goose",
	"opencode":     "OpenCode",
	"crush":        "Crush",
	"amp":          "Amp",
	"copilot":      "GitHub Copilot CLI",
	"ollama":       "Ollama",
}

func (c *collect) scheduledTasks() {
	switch c.env.OS {
	case "darwin":
		c.launchdJobs()
	case "windows":
		c.windowsTasks()
	default:
		c.systemdUnits()
		c.cronFiles()
	}
}

// system joins a machine-wide path onto the scanner's system root, which is
// empty on a real run and a fixture directory under test.
func (c *collect) system(parts ...string) string {
	root := c.sysRoot
	if root == "" && c.env.OS == "windows" {
		root = `C:\`
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

// launchdJobs reads the property lists launchd loads. User agents run as the
// logged in user; the ones under /Library run for everyone.
func (c *collect) launchdJobs() {
	dirs := []struct {
		path  string
		scope model.Scope
	}{
		{filepath.Join(c.env.HomeDir, "Library", "LaunchAgents"), model.ScopeUser},
		{c.system("Library", "LaunchAgents"), model.ScopeSystem},
		{c.system("Library", "LaunchDaemons"), model.ScopeSystem},
	}

	for _, d := range dirs {
		entries, err := readDirSorted(d.path)
		if err != nil {
			c.fail(d.path, err)
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".plist") {
				continue
			}
			path := filepath.Join(d.path, e.Name())
			b, err := readCapped(path)
			if err != nil {
				c.fail(abs(path), err)
				continue
			}
			job, err := parseLaunchdPlist(b)
			if errors.Is(err, errBinaryPlist) {
				// A binary property list is a different format. Reporting every
				// one of them would bury the user in notices about printer
				// drivers, so it is only reported when the raw bytes name an
				// agent this tool would otherwise have caught.
				if named := binaryPlistMentions(b); named != "" {
					c.failf(abs(path), "binary property list mentions "+named+" and was not parsed")
				}
				continue
			}
			if err != nil {
				c.fail(abs(path), err)
				continue
			}
			argv := job.argv()
			binary := matchAgentBinary(argv)
			if binary == "" {
				continue
			}

			rs := newReachSet()
			if len(argv) > 0 {
				rs.fromCommand(argv[0], argv[1:])
			}

			f := model.Finding{
				Kind:    model.KindScheduledTask,
				Name:    firstNonEmpty(job.Label, trimSuffixFold(e.Name(), ".plist")),
				Client:  clientForBinary[binary],
				Scope:   d.scope,
				Source:  abs(path),
				Command: strings.Join(argv, " "),
				Reach:   rs.list(),
				Digest:  digestOf("launchd", job.Label, strings.Join(argv, " "), strings.Join(job.Triggers, ",")),
			}
			f.Notes = append(f.Notes, "runs agent binary: "+binary)
			f.Notes = append(f.Notes, "launchd job, "+describeTriggers(job.Triggers))
			c.add(f)
		}
	}
}

// launchdJob is the part of a launchd property list that says what runs and
// when.
type launchdJob struct {
	Label            string
	Program          string
	ProgramArguments []string
	Triggers         []string
}

func (j launchdJob) argv() []string {
	if len(j.ProgramArguments) > 0 {
		return j.ProgramArguments
	}
	if j.Program != "" {
		return []string{j.Program}
	}
	return nil
}

// errBinaryPlist marks the one launchd format this tool does not read.
var errBinaryPlist = errors.New("binary property list, which this tool does not parse")

// binaryPlistMentions looks for an agent binary path inside the raw bytes of a
// property list that could not be parsed. Property lists store program paths as
// plain strings even in the binary format, so a leading slash keeps this from
// matching a word that merely contains an agent's name.
func binaryPlistMentions(b []byte) string {
	for _, name := range sortedKeys(agentBinaries) {
		if bytes.Contains(b, []byte("/"+name)) {
			return name
		}
	}
	return ""
}

// parseLaunchdPlist reads an XML property list. Binary property lists are a
// different format and are reported as unread rather than guessed at.
func parseLaunchdPlist(b []byte) (launchdJob, error) {
	var job launchdJob
	if bytes.HasPrefix(b, []byte("bplist00")) {
		return job, errBinaryPlist
	}

	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity

	var (
		dictDepth int
		inArray   bool
		key       string
	)
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return job, fmt.Errorf("property list is not readable XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				dictDepth++
				if dictDepth > 1 && isTriggerKey(key) {
					job.Triggers = appendUnique(job.Triggers, key)
				}
			case "array":
				inArray = true
				if dictDepth == 1 && isTriggerKey(key) {
					job.Triggers = appendUnique(job.Triggers, key)
				}
			case "key":
				var s string
				if err := dec.DecodeElement(&s, &t); err != nil {
					return job, err
				}
				if dictDepth == 1 {
					key = strings.TrimSpace(s)
				}
			case "string":
				var s string
				if err := dec.DecodeElement(&s, &t); err != nil {
					return job, err
				}
				if dictDepth != 1 {
					continue
				}
				switch {
				case inArray && key == "ProgramArguments":
					job.ProgramArguments = append(job.ProgramArguments, s)
				case !inArray && key == "Label":
					job.Label = s
				case !inArray && key == "Program":
					job.Program = s
				}
			case "true":
				if dictDepth == 1 && isTriggerKey(key) {
					job.Triggers = appendUnique(job.Triggers, key)
				}
			case "integer":
				var s string
				if err := dec.DecodeElement(&s, &t); err != nil {
					return job, err
				}
				if dictDepth == 1 && isTriggerKey(key) {
					job.Triggers = appendUnique(job.Triggers, key+"="+strings.TrimSpace(s))
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "dict":
				dictDepth--
			case "array":
				inArray = false
			}
		}
	}
	return job, nil
}

func isTriggerKey(key string) bool {
	switch key {
	case "RunAtLoad", "StartInterval", "StartCalendarInterval", "KeepAlive", "WatchPaths", "StartOnMount":
		return true
	}
	return false
}

// describeTriggers turns the launchd keys into a sentence, and says plainly
// when the property list declared none.
func describeTriggers(triggers []string) string {
	if len(triggers) == 0 {
		return "no start trigger declared in the property list"
	}
	return "starts on: " + strings.Join(triggers, ", ")
}

func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}

// systemdUnits reads the user's systemd units. A unit is only reported when its
// ExecStart names an agent binary.
func (c *collect) systemdUnits() {
	dirs := []struct {
		path  string
		scope model.Scope
	}{
		{filepath.Join(c.env.HomeDir, ".config", "systemd", "user"), model.ScopeUser},
		{c.system("etc", "systemd", "user"), model.ScopeSystem},
		{c.system("etc", "systemd", "system"), model.ScopeSystem},
	}

	for _, d := range dirs {
		entries, err := readDirSorted(d.path)
		if err != nil {
			c.fail(d.path, err)
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".service") {
				continue
			}
			path := filepath.Join(d.path, name)
			b, err := readCapped(path)
			if err != nil {
				c.fail(abs(path), err)
				continue
			}
			unit := parseUnit(b)
			argv := strings.Fields(unit["execstart"])
			binary := matchAgentBinary(argv)
			if binary == "" {
				continue
			}

			rs := newReachSet()
			if len(argv) > 0 {
				rs.fromCommand(argv[0], argv[1:])
			}

			timer := trimSuffixFold(name, ".service") + ".timer"
			trigger := "no matching timer unit; started by whatever wants it"
			if fileExists(filepath.Join(d.path, timer)) {
				trigger = "started by " + timer
			} else if unit["wantedby"] != "" {
				trigger = "wanted by " + unit["wantedby"]
			}

			f := model.Finding{
				Kind:    model.KindScheduledTask,
				Name:    trimSuffixFold(name, ".service"),
				Client:  clientForBinary[binary],
				Scope:   d.scope,
				Source:  abs(path),
				Command: unit["execstart"],
				Reach:   rs.list(),
				Digest:  digestOf("systemd", name, unit["execstart"], trigger),
			}
			f.Notes = append(f.Notes, "runs agent binary: "+binary)
			f.Notes = append(f.Notes, "systemd user unit, "+trigger)
			if unit["description"] != "" {
				f.Notes = append(f.Notes, "unit description: "+unit["description"])
			}
			c.add(f)
		}
	}
}

// parseUnit reads a systemd unit file into lower cased keys. Repeated keys keep
// the first value, which is enough for the one key that matters here.
func parseUnit(b []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if _, seen := out[key]; seen {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}

// cronFiles reads the crontab files that are readable without becoming another
// user. The per-user spool is usually not, and that is reported as an error
// against the path rather than passed over.
func (c *collect) cronFiles() {
	type cronSource struct {
		path     string
		scope    model.Scope
		hasUser  bool
		fromDir  bool
		fallback string
	}

	sources := []cronSource{
		{path: c.system("etc", "crontab"), scope: model.ScopeSystem, hasUser: true},
		{path: c.system("etc", "cron.d"), scope: model.ScopeSystem, hasUser: true, fromDir: true},
		{path: filepath.Join(c.system("var", "spool", "cron", "crontabs"), filepath.Base(c.env.HomeDir)), scope: model.ScopeUser},
	}

	for _, s := range sources {
		paths := []string{s.path}
		if s.fromDir {
			paths = nil
			entries, err := readDirSorted(s.path)
			if err != nil {
				c.fail(s.path, err)
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					paths = append(paths, filepath.Join(s.path, e.Name()))
				}
			}
		}
		for _, path := range paths {
			b, err := readCapped(path)
			if err != nil {
				c.fail(path, err)
				continue
			}
			for _, entry := range parseCrontab(string(b), s.hasUser) {
				binary := matchAgentBinary(strings.Fields(entry.command))
				if binary == "" {
					continue
				}
				argv := strings.Fields(entry.command)
				rs := newReachSet()
				if len(argv) > 0 {
					rs.fromCommand(argv[0], argv[1:])
				}
				f := model.Finding{
					Kind:    model.KindScheduledTask,
					Name:    binary + " in " + filepath.Base(path),
					Client:  clientForBinary[binary],
					Scope:   s.scope,
					Source:  abs(path),
					Command: entry.command,
					Reach:   rs.list(),
					Digest:  digestOf("cron", path, entry.schedule, entry.command),
				}
				f.Notes = append(f.Notes, "runs agent binary: "+binary)
				f.Notes = append(f.Notes, "cron entry, schedule: "+entry.schedule)
				c.add(f)
			}
		}
	}
}

type cronEntry struct {
	schedule string
	command  string
}

// parseCrontab splits crontab lines into a schedule and a command. System
// crontabs carry a user field between the two; a user's own crontab does not.
func parseCrontab(text string, hasUser bool) []cronEntry {
	var out []cronEntry
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// An assignment such as PATH=... is not a job.
		if i := strings.Index(line, "="); i > 0 && !strings.ContainsAny(line[:i], " \t") {
			continue
		}

		fields := strings.Fields(line)
		want := 5
		if strings.HasPrefix(line, "@") {
			want = 1
		}
		if hasUser {
			want++
		}
		if len(fields) <= want {
			continue
		}
		out = append(out, cronEntry{
			schedule: strings.Join(fields[:want], " "),
			command:  strings.Join(fields[want:], " "),
		})
	}
	return out
}

// winTask is the part of a Windows scheduled task definition that says what
// runs and when.
type winTask struct {
	RegistrationInfo struct {
		Author      string `xml:"Author"`
		Description string `xml:"Description"`
	} `xml:"RegistrationInfo"`
	Triggers struct {
		Logon    []struct{} `xml:"LogonTrigger"`
		Calendar []struct{} `xml:"CalendarTrigger"`
		Time     []struct{} `xml:"TimeTrigger"`
		Boot     []struct{} `xml:"BootTrigger"`
		Idle     []struct{} `xml:"IdleTrigger"`
	} `xml:"Triggers"`
	Actions struct {
		Exec []struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

func (t winTask) triggers() []string {
	var out []string
	if len(t.Triggers.Logon) > 0 {
		out = append(out, "at logon")
	}
	if len(t.Triggers.Calendar) > 0 {
		out = append(out, "on a calendar schedule")
	}
	if len(t.Triggers.Time) > 0 {
		out = append(out, "at a set time")
	}
	if len(t.Triggers.Boot) > 0 {
		out = append(out, "at boot")
	}
	if len(t.Triggers.Idle) > 0 {
		out = append(out, "when the machine is idle")
	}
	return out
}

// windowsTasks reads the task definitions the scheduler stores as files, plus
// the Startup folder. Shortcut targets are not resolved and the registry is not
// read; blindSpots says so.
func (c *collect) windowsTasks() {
	root := c.system("Windows", "System32", "Tasks")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			c.fail(path, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		b, err := readCapped(path)
		if err != nil {
			c.fail(abs(path), err)
			return nil
		}
		var task winTask
		if err := decodeTaskXML(b, &task); err != nil {
			// Not every file under Tasks is a task definition; only say so when
			// it looked like one.
			if bytes.Contains(b, []byte("<Task")) {
				c.failf(abs(path), "scheduled task definition is not readable XML: "+err.Error())
			}
			return nil
		}
		for _, exec := range task.Actions.Exec {
			argv := append([]string{exec.Command}, strings.Fields(exec.Arguments)...)
			binary := matchAgentBinary(argv)
			if binary == "" {
				continue
			}
			rs := newReachSet()
			rs.fromCommand(exec.Command, strings.Fields(exec.Arguments))

			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = filepath.Base(path)
			}
			triggers := task.triggers()
			f := model.Finding{
				Kind:      model.KindScheduledTask,
				Name:      rel,
				Client:    clientForBinary[binary],
				Scope:     model.ScopeSystem,
				Publisher: task.RegistrationInfo.Author,
				Source:    abs(path),
				Command:   strings.TrimSpace(exec.Command + " " + exec.Arguments),
				Reach:     rs.list(),
				Digest:    digestOf("windows-task", rel, exec.Command, exec.Arguments, strings.Join(triggers, ",")),
			}
			f.Notes = append(f.Notes, "runs agent binary: "+binary)
			if len(triggers) > 0 {
				f.Notes = append(f.Notes, "scheduled task, runs "+strings.Join(triggers, ", "))
			} else {
				f.Notes = append(f.Notes, "scheduled task with no trigger this tool recognises")
			}
			c.add(f)
		}
		return nil
	})

	c.startupFolder()
}

// startupFolder lists what Windows launches at login from the Start Menu. Only
// the file name is matched, because a .lnk holds its target in a binary format
// this tool does not open.
func (c *collect) startupFolder() {
	dir := filepath.Join(c.env.HomeDir, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	entries, err := readDirSorted(dir)
	if err != nil {
		c.fail(dir, err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		base := commandName(trimSuffixFold(name, ".lnk"))
		if !agentBinaries[base] {
			continue
		}
		path := filepath.Join(dir, name)
		f := model.Finding{
			Kind:   model.KindScheduledTask,
			Name:   name,
			Client: clientForBinary[base],
			Scope:  model.ScopeUser,
			Source: abs(path),
			Reach:  []model.Reach{model.ReachUnknown},
			Digest: digestOf("windows-startup", name),
		}
		f.Notes = append(f.Notes, "in the Startup folder, so it runs at login")
		f.Notes = append(f.Notes, "matched on the file name; the shortcut target is not read by this tool")
		c.add(f)
	}
}

// decodeTaskXML parses a Windows scheduled task definition. The bytes are
// converted from UTF-16 first, but the XML declaration inside them still says
// UTF-16, so the reader is handed back unchanged instead of being refused.
func decodeTaskXML(b []byte, v any) error {
	dec := xml.NewDecoder(bytes.NewReader(decodeUTF16(b)))
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	return dec.Decode(v)
}

// decodeUTF16 converts a UTF-16 file to UTF-8. Windows writes its scheduled
// task definitions as UTF-16 with a byte order mark, which an XML parser given
// the raw bytes will refuse.
func decodeUTF16(b []byte) []byte {
	switch {
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return utf16Bytes(b[2:], false)
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return utf16Bytes(b[2:], true)
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return b[3:]
	default:
		return b
	}
}

func utf16Bytes(b []byte, bigEndian bool) []byte {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			units = append(units, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return []byte(string(utf16.Decode(units)))
}
