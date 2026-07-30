package mcpservers

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// server is one MCP server declaration, normalised out of whichever shape the
// client's config file happens to use.
//
// It holds environment variable NAMES and never values. An MCP server's env
// block is where API keys live, so the values are dropped at parse time: they
// are not stored, not hashed and not printed.
type server struct {
	name       string
	command    string
	args       []string
	envKeys    []string
	url        string
	transport  string // the declared type, verbatim, e.g. "stdio", "http", "sse"
	headerKeys []string
	toolNames  []string
	// autoApprove are tools the config lets the server run without asking.
	autoApprove []string
	disabled    bool

	// scope overrides the client row's scope. Claude Code keeps both user and
	// per-project servers in one file, so the shape decides, not the row.
	scope   model.Scope
	project string
}

// shapeFunc reads one config file and returns the servers it declares.
type shapeFunc func(data []byte) ([]server, error)

// rawServer covers the union of the per-server object across every client in
// the table. Fields no client uses simply stay empty.
type rawServer struct {
	Command     any            `json:"command"` // string, or Zed's {path,args,env} object
	Args        []string       `json:"args"`
	Env         map[string]any `json:"env"`
	URL         string         `json:"url"`
	ServerURL   string         `json:"serverUrl"`
	Type        string         `json:"type"`
	Headers     map[string]any `json:"headers"`
	Disabled    *bool          `json:"disabled"`
	Enabled     *bool          `json:"enabled"`
	Tools       map[string]any `json:"tools"`
	AlwaysAllow []string       `json:"alwaysAllow"`
	AutoApprove []string       `json:"autoApprove"`
	Transport   *rawTransport  `json:"transport"`
}

// rawTransport is Continue's older nesting, where the command lives one level
// down instead of on the server object.
type rawTransport struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	URL     string   `json:"url"`
}

// zedCommand is the older Zed form, where command is an object rather than a
// string. Both forms are still accepted by Zed, so both are read here.
type zedCommand struct {
	Path string         `json:"path"`
	Args []string       `json:"args"`
	Env  map[string]any `json:"env"`
}

func (r rawServer) normalise(name string) server {
	s := server{
		name:      name,
		args:      append([]string(nil), r.Args...),
		url:       firstNonEmpty(r.URL, r.ServerURL),
		transport: r.Type,
	}

	switch c := r.Command.(type) {
	case string:
		s.command = c
	case map[string]any:
		var zc zedCommand
		if b, err := json.Marshal(c); err == nil {
			_ = json.Unmarshal(b, &zc)
		}
		s.command = zc.Path
		if len(zc.Args) > 0 {
			s.args = append(s.args, zc.Args...)
		}
		s.envKeys = append(s.envKeys, keysOf(zc.Env)...)
	}

	if r.Transport != nil {
		if s.command == "" {
			s.command = r.Transport.Command
		}
		if len(s.args) == 0 {
			s.args = append([]string(nil), r.Transport.Args...)
		}
		if s.url == "" {
			s.url = r.Transport.URL
		}
		if s.transport == "" {
			s.transport = r.Transport.Type
		}
	}

	s.envKeys = append(s.envKeys, keysOf(r.Env)...)
	s.headerKeys = keysOf(r.Headers)
	s.toolNames = append(s.toolNames, keysOf(r.Tools)...)
	s.autoApprove = append(s.autoApprove, r.AlwaysAllow...)
	s.autoApprove = append(s.autoApprove, r.AutoApprove...)
	s.toolNames = append(s.toolNames, s.autoApprove...)

	if r.Disabled != nil && *r.Disabled {
		s.disabled = true
	}
	if r.Enabled != nil && !*r.Enabled {
		s.disabled = true
	}

	sort.Strings(s.envKeys)
	sort.Strings(s.headerKeys)
	sort.Strings(s.toolNames)
	sort.Strings(s.autoApprove)
	s.envKeys = dedupe(s.envKeys)
	s.headerKeys = dedupe(s.headerKeys)
	s.toolNames = dedupe(s.toolNames)
	s.autoApprove = dedupe(s.autoApprove)
	return s
}

// shapeMCPServers reads the most common shape: a top level "mcpServers" object
// keyed by server name. Claude Desktop, Claude Code project files, Cursor,
// Windsurf, Cline and Gemini CLI all use it.
func shapeMCPServers(data []byte) ([]server, error) {
	var doc struct {
		MCPServers map[string]rawServer `json:"mcpServers"`
	}
	if err := decodeJSONC(data, &doc); err != nil {
		return nil, err
	}
	return fromMap(doc.MCPServers), nil
}

// shapeServers reads VS Code's mcp.json, which keys the same objects under
// "servers" and carries a separate "inputs" list this scanner does not read.
func shapeServers(data []byte) ([]server, error) {
	var doc struct {
		Servers map[string]rawServer `json:"servers"`
	}
	if err := decodeJSONC(data, &doc); err != nil {
		return nil, err
	}
	return fromMap(doc.Servers), nil
}

// shapeVSCodeSettings reads the older VS Code placement, where the same block
// sat inside settings.json under an "mcp" key. Machines upgraded from an
// earlier VS Code still have servers here.
func shapeVSCodeSettings(data []byte) ([]server, error) {
	var doc struct {
		MCP struct {
			Servers map[string]rawServer `json:"servers"`
		} `json:"mcp"`
	}
	if err := decodeJSONC(data, &doc); err != nil {
		return nil, err
	}
	return fromMap(doc.MCP.Servers), nil
}

// shapeContextServers reads Zed, which calls them context servers and allows
// the command to be either a string or an object.
func shapeContextServers(data []byte) ([]server, error) {
	var doc struct {
		ContextServers map[string]rawServer `json:"context_servers"`
	}
	if err := decodeJSONC(data, &doc); err != nil {
		return nil, err
	}
	return fromMap(doc.ContextServers), nil
}

// shapeContinueLegacy reads Continue's deprecated config.json, where servers
// are a list under experimental.modelContextProtocolServers and the command is
// nested inside a transport object.
func shapeContinueLegacy(data []byte) ([]server, error) {
	var doc struct {
		Experimental struct {
			Servers []rawServer `json:"modelContextProtocolServers"`
		} `json:"experimental"`
	}
	if err := decodeJSONC(data, &doc); err != nil {
		return nil, err
	}
	out := make([]server, 0, len(doc.Experimental.Servers))
	for i, r := range doc.Experimental.Servers {
		name := ""
		if r.Transport != nil {
			name = r.Transport.Command
		}
		if name == "" {
			name = fmt.Sprintf("server %d", i+1)
		}
		out = append(out, r.normalise(name))
	}
	return out, nil
}

// shapeClaudeCodeUser reads ~/.claude.json. That one file holds both the user
// scoped servers and a per-project block for every directory Claude Code has
// been run in, so it produces findings in two scopes.
func shapeClaudeCodeUser(data []byte) ([]server, error) {
	var doc struct {
		MCPServers map[string]rawServer `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]rawServer `json:"mcpServers"`
		} `json:"projects"`
	}
	if err := decodeJSONC(data, &doc); err != nil {
		return nil, err
	}

	out := fromMap(doc.MCPServers)
	for i := range out {
		out[i].scope = model.ScopeUser
	}

	projects := make([]string, 0, len(doc.Projects))
	for dir := range doc.Projects {
		projects = append(projects, dir)
	}
	sort.Strings(projects)
	for _, dir := range projects {
		for _, s := range fromMap(doc.Projects[dir].MCPServers) {
			s.scope = model.ScopeProject
			s.project = dir
			out = append(out, s)
		}
	}
	return out, nil
}

// shapeJetBrainsXML is a discovery parser rather than a schema parser.
//
// JetBrains documents how to add an MCP server through the AI Assistant
// settings dialog but does not document where that lands on disk, so this reads
// any mcp*.xml under the IDE options directory and pulls out the embedded JSON
// configuration the dialog stores. The matching gap says so out loud, because
// a config stored under some other name will be missed.
func shapeJetBrainsXML(data []byte) ([]server, error) {
	text := html.UnescapeString(string(data))
	block, ok := enclosingJSONObject(text, `"mcpServers"`)
	if !ok {
		return nil, nil
	}
	return shapeMCPServers([]byte(block))
}

// enclosingJSONObject finds needle in text and returns the smallest balanced
// { } block that contains it.
func enclosingJSONObject(text, needle string) (string, bool) {
	at := strings.Index(text, needle)
	if at < 0 {
		return "", false
	}
	start := strings.LastIndex(text[:at], "{")
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	for i := start; i < len(text); i++ {
		c := text[i]
		if inString {
			switch c {
			case '\\':
				i++
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1], true
			}
		}
	}
	return "", false
}

func fromMap(m map[string]rawServer) []server {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]server, 0, len(names))
	for _, name := range names {
		out = append(out, m[name].normalise(name))
	}
	return out
}

// keysOf returns the map's keys and drops every value. This is the one place
// that guarantees a secret in an env block never leaves the file it sits in.
func keysOf(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dedupe(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
