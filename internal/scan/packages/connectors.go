package packages

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// A connector is a remote endpoint a client is configured to talk to: an entry
// in the client's configuration that declares a URL instead of a command to
// run. It is separated from a local model context protocol server here because
// the two fail differently. A local server is code on this machine; a connector
// is a name pointing somewhere else, and whoever answers at that address can
// change without anything on this machine changing.
//
// Only the declared endpoint is read. Headers and environment blocks in the
// same entry hold tokens, so their names are counted and their values are never
// opened.

type declaredServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	URL     string            `json:"url"`
	HTTPURL string            `json:"httpUrl"`
	Headers map[string]string `json:"headers"`
	Env     map[string]string `json:"env"`
}

type clientConfig struct {
	MCPServers map[string]declaredServer `json:"mcpServers"`
	Connectors map[string]declaredServer `json:"connectors"`
}

func (c *collect) connectors() {
	// Claude Desktop keeps its configuration file beside its extensions.
	desktop := filepath.Join(c.claudeUserData(), "claude_desktop_config.json")
	c.connectorsIn(desktop, clientClaudeDesktop, model.ScopeUser)

	// A project can declare its own, and those arrive with a clone rather than
	// by anyone choosing them.
	for _, root := range c.env.Roots {
		c.connectorsIn(filepath.Join(root, ".mcp.json"), clientClaudeCode, model.ScopeProject)
	}
}

func (c *collect) connectorsIn(path, client string, scope model.Scope) {
	b, err := readCapped(path)
	if err != nil {
		c.fail(path, err)
		return
	}
	var cfg clientConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		c.fail(abs(path), err)
		return
	}

	groups := []struct {
		label   string
		servers map[string]declaredServer
	}{
		{"mcpServers", cfg.MCPServers},
		{"connectors", cfg.Connectors},
	}

	for _, g := range groups {
		for _, name := range sortedKeys(g.servers) {
			s := g.servers[name]
			endpoint := firstNonEmpty(s.HTTPURL, s.URL)
			// An entry with a command is a local server, and the model context
			// protocol scanner reports those. Only remote entries are
			// connectors.
			if endpoint == "" || s.Command != "" {
				continue
			}

			rs := newReachSet()
			rs.add(model.ReachNetwork)

			f := model.Finding{
				Kind:    model.KindConnector,
				Name:    name,
				Client:  client,
				Scope:   scope,
				Source:  abs(path),
				Command: endpoint,
				Reach:   rs.list(),
				Digest:  digestOf("connector", name, s.Type, endpoint),
			}
			f.Notes = append(f.Notes, "declared under "+g.label+" as a remote endpoint: "+endpoint)
			if host := hostOf(endpoint); host != "" {
				f.Notes = append(f.Notes, "host: "+host)
			}
			if len(s.Headers) > 0 {
				f.Notes = append(f.Notes, "sends "+strings.Join(sortedKeys(s.Headers), ", ")+" (header values are not read by this tool)")
			}
			if len(s.Env) > 0 {
				f.Notes = append(f.Notes, "declares environment variables: "+strings.Join(sortedKeys(s.Env), ", ")+" (values are not read by this tool)")
			}
			c.add(f)
		}
	}
}

// hostOf pulls the host out of a declared endpoint without parsing a URL, which
// keeps a malformed string from turning into an error the user cannot act on.
func hostOf(endpoint string) string {
	s := endpoint
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "@"); i >= 0 {
		// Never carry credentials that were written into a URL.
		s = s[i+1:]
	}
	return s
}
