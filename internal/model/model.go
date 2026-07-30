// Package model holds the shared vocabulary of the inventory: what a finding
// is, what a scanner is, and what a completed run looks like.
//
// Every scanner in this tool reads local files and nothing else. There is no
// network client here, and the release build fails if one appears.
package model

// Kind is the class of agent machinery a finding describes.
type Kind string

const (
	KindMCPServer        Kind = "mcp_server"
	KindExtension        Kind = "extension"
	KindPlugin           Kind = "plugin"
	KindSkill            Kind = "skill"
	KindConnector        Kind = "connector"
	KindInstructionFile  Kind = "instruction_file"
	KindBrowserExtension Kind = "browser_extension"
	KindScheduledTask    Kind = "scheduled_task"
)

// Scope is whether a finding applies to the whole user account or to one
// project directory. Project scope matters because a repository can carry
// agent configuration that the user never installed themselves.
type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeSystem  Scope = "system"
)

// Reach is a capability the item can use once it runs. These are stated as
// observed facts about the declared configuration, never as a judgement.
type Reach string

const (
	ReachShell       Reach = "shell"
	ReachAppleScript Reach = "applescript"
	ReachFilesystem  Reach = "filesystem"
	ReachNetwork     Reach = "network"
	ReachBrowserTabs Reach = "browser_tabs"
	ReachClipboard   Reach = "clipboard"
	ReachCredentials Reach = "credentials"
	ReachUnknown     Reach = "unknown"
)

// CatalogueMatch is what a shipped catalogue snapshot knows about an item.
// Absent match is normal and is reported as unknown rather than as safe.
type CatalogueMatch struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Publisher string `json:"publisher,omitempty"`
	Verified  bool   `json:"verified"`
}

// Finding is one piece of agent machinery found on this machine.
type Finding struct {
	Kind      Kind            `json:"kind"`
	Name      string          `json:"name"`
	Client    string          `json:"client,omitempty"`
	Scope     Scope           `json:"scope"`
	Publisher string          `json:"publisher,omitempty"`
	Version   string          `json:"version,omitempty"`
	Source    string          `json:"source"`
	Command   string          `json:"command,omitempty"`
	Reach     []Reach         `json:"reach,omitempty"`
	Catalogue *CatalogueMatch `json:"catalogue,omitempty"`
	// Digest is a stable hash of the parts of this item that should not change
	// on their own. Drift detection compares digests between runs.
	Digest string `json:"digest,omitempty"`
	// Notes are plain factual observations, never verdicts.
	Notes []string `json:"notes,omitempty"`
}

// Gap records something the run deliberately or unavoidably did not look at.
// Every run prints these, because a tool that hides its blind spots is a claim
// rather than an inventory.
type Gap struct {
	Area   string `json:"area"`
	Reason string `json:"reason"`
}

// ScanError is a failure in one scanner. One scanner failing never aborts the
// run; the failure is reported alongside the findings.
type ScanError struct {
	Scanner string `json:"scanner"`
	Path    string `json:"path,omitempty"`
	Err     string `json:"error"`
}

// Env is the machine a scan runs against. It is injected so that every scanner
// is testable against fixture directories rather than the developer's own home
// directory.
type Env struct {
	OS      string
	HomeDir string
	// Roots are the project directories to scan for project-scoped config.
	// Empty means the current working directory only.
	Roots []string
}

// Scanner is one detector. Scan reads only from env and returns what it found,
// what it could not see, and any errors it survived.
type Scanner interface {
	Name() string
	Scan(env Env) ([]Finding, []Gap, []ScanError)
}

// Result is one complete run.
type Result struct {
	Tool     string      `json:"tool"`
	Version  string      `json:"version"`
	OS       string      `json:"os"`
	Findings []Finding   `json:"findings"`
	Gaps     []Gap       `json:"gaps"`
	Errors   []ScanError `json:"errors,omitempty"`
	// Drift is populated when a previous baseline exists.
	Drift []DriftEntry `json:"drift,omitempty"`
}

// DriftEntry records an item whose declared definition changed since the last
// run on this machine. This is the rug-pull case: a tool the user already
// approved, quietly redefined.
type DriftEntry struct {
	Name     string `json:"name"`
	Kind     Kind   `json:"kind"`
	Source   string `json:"source"`
	WasHash  string `json:"was"`
	NowHash  string `json:"now"`
	Detected string `json:"first_seen_changed,omitempty"`
}
