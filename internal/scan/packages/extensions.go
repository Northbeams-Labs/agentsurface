package packages

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

// A desktop extension is a packaged local MCP server: a zip named .mcpb (or
// .dxt, the earlier name for the same thing) holding a manifest.json and the
// server it describes. Claude Desktop unpacks each one into
// join(userData, "Claude Extensions")/<extension id>/ and records the install
// in extensions-installations.json next to it.
//
// The manifest format is the MCPB manifest spec, currently version 0.3. The
// fields read here are the declared ones: who wrote it, what it runs, what
// tools it says it has, and what configuration it asks the user for. The
// values the user supplied are deliberately never read.

type mcpbManifest struct {
	ManifestVersion string `json:"manifest_version"`
	DXTVersion      string `json:"dxt_version"`
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	Version         string `json:"version"`
	Description     string `json:"description"`
	Author          person `json:"author"`
	Server          struct {
		Type       string `json:"type"`
		EntryPoint string `json:"entry_point"`
		MCPConfig  struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcp_config"`
	} `json:"server"`
	Tools         []declaredTool             `json:"tools"`
	UserConfig    map[string]userConfigField `json:"user_config"`
	Compatibility struct {
		Platforms []string `json:"platforms"`
	} `json:"compatibility"`
}

// declaredTool is one tool a manifest says the server offers. The name is the
// part that matters here: a tool called execute_command has told you what it is.
type declaredTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// userConfigField describes a value an extension asks the user for. Only the
// shape is read. Default is not read even from the manifest, so that there is
// no code path in this scanner that can reach a configured value.
type userConfigField struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Required  bool   `json:"required"`
	Sensitive bool   `json:"sensitive"`
}

// person is an author block. The spec says it is an object, but manifests in
// the wild sometimes carry a bare string, so both are accepted.
type person struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Email string `json:"email"`
}

func (p *person) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		p.Name = s
		return nil
	}
	type alias person
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		// An unreadable author is not a reason to lose the rest of a manifest.
		return nil
	}
	*p = person(a)
	return nil
}

// extensionInstall is the client's own record of an install: the version it
// believes is there, the package hash it recorded, and whether the bundle
// carried a signature.
type extensionInstall struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	Hash          string `json:"hash"`
	InstalledAt   string `json:"installedAt"`
	Source        string `json:"source"`
	SignatureInfo struct {
		Status    string `json:"status"`
		Publisher string `json:"publisher"`
	} `json:"signatureInfo"`
}

type extensionInstalls struct {
	Extensions map[string]extensionInstall `json:"extensions"`
}

// extensionSettings is the per-extension settings file. Only isEnabled is read;
// the userConfig block beside it holds the values the user typed, which include
// API keys, and this scanner does not open it.
type extensionSettings struct {
	IsEnabled *bool `json:"isEnabled"`
}

func (c *collect) desktopExtensions() {
	userData := c.claudeUserData()
	dir := filepath.Join(userData, "Claude Extensions")

	installs := extensionInstalls{}
	installPath := filepath.Join(userData, "extensions-installations.json")
	if err := readJSON(installPath, &installs); err != nil {
		c.fail(installPath, err)
	}

	entries, err := readDirSorted(dir)
	if err != nil {
		c.fail(dir, err)
		return
	}

	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() {
			c.unpackedExtension(userData, path, e.Name(), installs)
			continue
		}
		if isBundleName(e.Name()) {
			c.bundleExtension(path)
		}
	}
}

// unpackedExtension reads one installed extension directory.
func (c *collect) unpackedExtension(userData, dir, id string, installs extensionInstalls) {
	manifestPath := filepath.Join(dir, "manifest.json")
	var m mcpbManifest
	if err := readJSON(manifestPath, &m); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.failf(abs(dir), "installed extension directory has no manifest.json")
			return
		}
		c.fail(abs(manifestPath), err)
		return
	}

	f := findingFromManifest(m, abs(dir))
	f.Notes = append(f.Notes, "installed extension id: "+id)

	if inst, ok := installs.Extensions[id]; ok {
		f.Notes = append(f.Notes, "signature: "+signatureLabel(inst.SignatureInfo.Status))
		if inst.SignatureInfo.Publisher != "" {
			f.Notes = append(f.Notes, "signature publisher: "+inst.SignatureInfo.Publisher)
		}
		if inst.Source != "" {
			f.Notes = append(f.Notes, "install source: "+inst.Source)
		}
		if inst.InstalledAt != "" {
			f.Notes = append(f.Notes, "installed at: "+inst.InstalledAt)
		}
		if inst.Hash != "" {
			f.Notes = append(f.Notes, "package hash recorded by the client: "+inst.Hash)
		}
	} else {
		f.Notes = append(f.Notes, "signature: "+signatureLabel(""))
		f.Notes = append(f.Notes, "the client has no install record for this directory")
	}

	settingsPath := filepath.Join(userData, "Claude Extensions Settings", id+".json")
	var st extensionSettings
	if err := readJSON(settingsPath, &st); err != nil {
		c.fail(settingsPath, err)
	} else if st.IsEnabled != nil {
		if *st.IsEnabled {
			f.Notes = append(f.Notes, "enabled in the client")
		} else {
			f.Notes = append(f.Notes, "present but switched off in the client")
		}
	}

	c.add(f)
}

// bundleFiles looks for packaged extensions that are on the machine but not
// installed: the file sitting in Downloads after someone clicked one. It is
// still agent machinery a click away from running.
func (c *collect) bundleFiles() {
	dir := filepath.Join(c.env.HomeDir, "Downloads")
	entries, err := readDirSorted(dir)
	if err != nil {
		c.fail(dir, err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !isBundleName(e.Name()) {
			continue
		}
		c.bundleExtension(filepath.Join(dir, e.Name()))
	}
}

// bundleExtension reads manifest.json straight out of a .mcpb or .dxt zip
// without unpacking it. A bundle that will not open is reported and the run
// carries on.
func (c *collect) bundleExtension(path string) {
	m, err := bundleManifest(path)
	if err != nil {
		c.failf(abs(path), "cannot read extension bundle: "+err.Error())
		return
	}
	f := findingFromManifest(m, abs(path))
	f.Notes = append(f.Notes, "packaged bundle on disk, not an installed extension directory")
	f.Notes = append(f.Notes, "signature: "+signatureLabel(""))
	c.add(f)
}

func bundleManifest(path string) (mcpbManifest, error) {
	var m mcpbManifest
	zr, err := zip.OpenReader(path)
	if err != nil {
		return m, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if filepath.ToSlash(zf.Name) != "manifest.json" {
			continue
		}
		if zf.UncompressedSize64 > maxFileBytes {
			return m, errors.New("manifest.json is larger than this scanner reads")
		}
		rc, err := zf.Open()
		if err != nil {
			return m, err
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, maxFileBytes))
		if err != nil {
			return m, err
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return m, fmt.Errorf("manifest.json is not valid JSON: %w", err)
		}
		return m, nil
	}
	return m, errors.New("bundle contains no manifest.json")
}

// findingFromManifest turns a manifest into a finding. Everything in it is a
// declared fact with a path behind it.
func findingFromManifest(m mcpbManifest, source string) model.Finding {
	name := m.DisplayName
	if name == "" {
		name = m.Name
	}
	if name == "" {
		name = filepath.Base(source)
	}

	cfg := m.Server.MCPConfig
	rs := newReachSet()
	rs.fromCommand(cfg.Command, cfg.Args)
	rs.fromCommand(m.Server.EntryPoint, nil)
	for _, t := range m.Tools {
		rs.fromTool(t.Name)
	}
	for _, key := range sortedKeys(m.UserConfig) {
		rs.fromUserConfigType(m.UserConfig[key].Type)
	}
	// Declared endpoints in the environment block count as network reach. The
	// values are inspected for the scheme only and are never recorded.
	for _, key := range sortedKeys(cfg.Env) {
		if hasURL(strings.ToLower(cfg.Env[key])) {
			rs.add(model.ReachNetwork)
		}
	}

	f := model.Finding{
		Kind:      model.KindExtension,
		Name:      name,
		Client:    clientClaudeDesktop,
		Scope:     model.ScopeUser,
		Publisher: m.Author.Name,
		Version:   m.Version,
		Source:    source,
		Command:   declaredCommand(cfg.Command, cfg.Args),
		Reach:     rs.list(),
	}

	f.Notes = append(f.Notes, "manifest format: "+manifestFormat(m))
	if m.Server.Type != "" || m.Server.EntryPoint != "" {
		f.Notes = append(f.Notes, fmt.Sprintf("declared entry point: %s %s", m.Server.Type, m.Server.EntryPoint))
	}
	if len(m.Tools) > 0 {
		f.Notes = append(f.Notes, "declares "+summariseTools(m.Tools))
	}
	if len(m.UserConfig) > 0 {
		f.Notes = append(f.Notes, "asks the user for "+summariseUserConfig(m.UserConfig)+" (the values are not read by this tool)")
	}
	if len(m.Compatibility.Platforms) > 0 {
		f.Notes = append(f.Notes, "declares platforms: "+strings.Join(m.Compatibility.Platforms, ", "))
	}

	f.Digest = digestOf(append([]string{
		"mcpb",
		m.Name,
		m.Version,
		m.Server.Type,
		m.Server.EntryPoint,
		cfg.Command,
		strings.Join(cfg.Args, " "),
		strings.Join(toolNames(m.Tools), ","),
		strings.Join(userConfigShape(m.UserConfig), ","),
	}, sortedKeys(cfg.Env)...)...)

	return f
}

func manifestFormat(m mcpbManifest) string {
	switch {
	case m.ManifestVersion != "":
		return "mcpb manifest_version " + m.ManifestVersion
	case m.DXTVersion != "":
		return "dxt_version " + m.DXTVersion
	default:
		return "no manifest version declared"
	}
}

// signatureLabel keeps the unsigned case explicit. A missing signature block is
// reported as not recorded rather than as signed or as unsigned, because the
// absence of a record is not the same as either.
func signatureLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return "not recorded by the client"
	case "unsigned":
		return "unsigned"
	default:
		return status
	}
}

func toolNames(tools []declaredTool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func summariseTools(tools []declaredTool) string {
	names := toolNames(tools)
	shown := names
	extra := ""
	if len(shown) > 6 {
		shown = shown[:6]
		extra = fmt.Sprintf(" and %d more", len(names)-6)
	}
	return fmt.Sprintf("%s: %s%s", plural(len(names), "tool"), strings.Join(shown, ", "), extra)
}

func userConfigShape(uc map[string]userConfigField) []string {
	out := make([]string, 0, len(uc))
	for _, k := range sortedKeys(uc) {
		out = append(out, k+":"+uc[k].Type)
	}
	return out
}

func summariseUserConfig(uc map[string]userConfigField) string {
	parts := make([]string, 0, len(uc))
	for _, k := range sortedKeys(uc) {
		v := uc[k]
		bits := v.Type
		if v.Required {
			bits += ", required"
		}
		if v.Sensitive {
			bits += ", marked sensitive"
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", k, bits))
	}
	return strings.Join(parts, "; ")
}

// declaredCommand renders the entry point exactly as the manifest declares it,
// placeholders and all, because ${__dirname} is what is written down.
func declaredCommand(cmd string, args []string) string {
	if cmd == "" {
		return ""
	}
	return strings.TrimSpace(cmd + " " + strings.Join(args, " "))
}

func isBundleName(name string) bool {
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".mcpb") || strings.HasSuffix(low, ".dxt")
}

// geminiExtension is the Gemini CLI extension manifest. Gemini CLI loads
// extensions from <home>/.gemini/extensions, each with a gemini-extension.json
// in its root.
type geminiExtension struct {
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	Description     string   `json:"description"`
	ContextFileName string   `json:"contextFileName"`
	ExcludeTools    []string `json:"excludeTools"`
	MCPServers      map[string]struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		URL     string            `json:"url"`
		HTTPURL string            `json:"httpUrl"`
		Env     map[string]string `json:"env"`
	} `json:"mcpServers"`
}

func (c *collect) geminiExtensions() {
	dir := filepath.Join(c.env.HomeDir, ".gemini", "extensions")
	entries, err := readDirSorted(dir)
	if err != nil {
		c.fail(dir, err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		extDir := filepath.Join(dir, e.Name())
		manifestPath := filepath.Join(extDir, "gemini-extension.json")
		var g geminiExtension
		if err := readJSON(manifestPath, &g); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			c.fail(abs(manifestPath), err)
			continue
		}

		name := g.Name
		if name == "" {
			name = e.Name()
		}
		rs := newReachSet()
		digest := []string{"gemini-extension", g.Name, g.Version, g.ContextFileName}
		for _, key := range sortedKeys(g.MCPServers) {
			s := g.MCPServers[key]
			rs.fromCommand(s.Command, s.Args)
			if s.URL != "" || s.HTTPURL != "" {
				rs.add(model.ReachNetwork)
			}
			digest = append(digest, key, s.Command, strings.Join(s.Args, " "), s.URL, s.HTTPURL)
		}

		f := model.Finding{
			Kind:    model.KindExtension,
			Name:    name,
			Client:  clientGeminiCLI,
			Scope:   model.ScopeUser,
			Version: g.Version,
			Source:  abs(extDir),
			Reach:   rs.list(),
			Digest:  digestOf(digest...),
		}
		f.Notes = append(f.Notes, "signature: "+signatureLabel(""))
		if len(g.MCPServers) > 0 {
			f.Notes = append(f.Notes, "declares model context protocol servers: "+strings.Join(sortedKeys(g.MCPServers), ", "))
		}
		if g.ContextFileName != "" {
			f.Notes = append(f.Notes, "loads context file: "+g.ContextFileName)
		}
		if len(g.ExcludeTools) > 0 {
			f.Notes = append(f.Notes, "declares excluded tools: "+strings.Join(g.ExcludeTools, ", "))
		}
		c.add(f)
	}
}
