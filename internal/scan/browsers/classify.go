package browsers

import (
	"fmt"
	"regexp"
	"strings"
)

// knownAI is a shipped snapshot of extension IDs that are AI assistants,
// agents or chatbots. It is a snapshot, not an authority: IDs change, new
// assistants ship weekly, and an extension can be renamed. That is why an ID
// match is only one of four signals, and why the run records a gap saying the
// list goes stale.
//
// Verified entries were confirmed against the Chrome Web Store listing by
// earlier internal work; unverified entries are carried from public listings
// and are reported as unverified rather than presented as fact.
//
// Two deliberate absences: Microsoft Copilot ships inside Edge and Google
// Gemini ships inside Chrome, so neither has an official extension to find.
// The store entries using those names are third party, and listing them would
// mean accusing a user of running something they are not.
type catalogueEntry struct {
	Name      string
	Publisher string
	Verified  bool
}

var knownAI = map[string]catalogueEntry{
	// Anthropic
	"fcoeoabgfenejglbffodgkkbkcdhcgfn": {Name: "Claude for Chrome", Publisher: "Anthropic", Verified: true},
	// OpenAI
	"hehggadaopoacecdllhhajmbjkdcmajg": {Name: "ChatGPT for Chrome", Publisher: "OpenAI", Verified: true},
	// Grammarly-class writing assistants
	"kbfnbcaeplbcioakkpcpgfkobkghlhen": {Name: "Grammarly: AI Writing Assistant", Publisher: "Grammarly", Verified: true},
	"ddlbpiadoechcolndfeaonajmngmhblj": {Name: "Compose AI", Publisher: "Compose AI"},
	// Perplexity
	"bbjbdkonlhomnpandcpiiglkkbdmjmpb": {Name: "Perplexity - AI Companion", Publisher: "Perplexity AI"},
	"hlgbcneanomplepojfcnclggenpcoldo": {Name: "Perplexity - AI Search", Publisher: "Perplexity AI"},
	// Prominent independents
	"difoiogjjojoaoomphldepapgpbgkhkb": {Name: "Sider: ChatGPT Sidebar", Publisher: "Sider.AI"},
	"ofpnmcalabcbjgholdjcjblkibolbppb": {Name: "Monica - AI Assistant", Publisher: "Monica"},
	"eanggfilgoajaocelnaflolkadkeghjp": {Name: "HARPA AI Agent", Publisher: "HARPA AI"},
	"dhlpobdgcjafebgbbhjdnapejmpkgiie": {Name: "MaxAI.me", Publisher: "MaxAI"},
	"camppjleccjaphfdbohjdohecfnoikec": {Name: "Merlin AI Assistant", Publisher: "Foyer"},
	"ojnbohmppadfgpejeebfnmnknjdlckgj": {Name: "AIPRM for ChatGPT", Publisher: "AIPRM"},
	"lpfemeioodjbpieminkklglpmhlngfcn": {Name: "WebChatGPT", Publisher: "WebChatGPT"},
	"amhmeenmapldpjdedekalnfifgnpfnkc": {Name: "Superpower ChatGPT", Publisher: "Superpower"},
	// Firefox add-on ids look nothing like Chromium ids, so they live here too.
	"87677a2c52b84ad3a151a4a72f5bd3c4@jetpack": {Name: "Grammarly for Firefox", Publisher: "Grammarly"},
}

// aiTermRE matches the vocabulary an assistant uses to describe itself. The
// terms are anchored on word boundaries because substring matching turns
// "Trainer" and "Maintain" into AI extensions.
var aiTermRE = regexp.MustCompile(`(?i)\b(ai|a\.i\.|artificial intelligence|agentic|ai agent|ai assistant|browser agent|chatbot|chat bot|chatgpt|gpt-?[0-9]*|claude|anthropic|openai|gemini|bard|copilot|perplexity|llm|large language model|prompt|deepseek|mistral|grok|sidekick|writing assistant|summari[sz]e[rd]?)\b`)

// agentBridgeRE matches native messaging host names that read as a bridge
// from a browser extension to a local model or agent process.
var agentBridgeRE = regexp.MustCompile(`(?i)(^|[._-])(ai|agent|assistant|claude|anthropic|openai|chatgpt|gpt|copilot|gemini|llm|ollama|perplexity|codex|cursor|mcp)([._-]|$)`)

// signal is one reason an extension was reported, recorded verbatim in the
// finding's notes so a reader can disagree with the classifier.
type signal struct {
	Kind string // known-id, name-match, native-host, permission-shape
	Note string
}

// classify decides whether an extension is AI-aware and says why. Any signal
// is enough to report it; no signal means it is not reported at all.
func classify(e extension) (entry *catalogueEntry, signals []signal) {
	if c, ok := knownAI[e.ID]; ok {
		entry = &c
		signals = append(signals, signal{
			Kind: "known-id",
			Note: fmt.Sprintf("ai signal: known-id, shipped list names %s as %q by %s", e.ID, c.Name, c.Publisher),
		})
	}
	if m := aiTermRE.FindString(e.Name); m != "" {
		signals = append(signals, signal{
			Kind: "name-match",
			Note: fmt.Sprintf("ai signal: name-match, extension name contains %q", m),
		})
	} else if m := aiTermRE.FindString(e.Description); m != "" {
		signals = append(signals, signal{
			Kind: "name-match",
			Note: fmt.Sprintf("ai signal: name-match, declared description contains %q", m),
		})
	}
	for _, h := range e.NativeHosts {
		if agentBridgeRE.MatchString(h) {
			signals = append(signals, signal{
				Kind: "native-host",
				Note: fmt.Sprintf("ai signal: native-host, native messaging host %q reads as an agent bridge to a local binary", h),
			})
		}
	}
	if why, ok := assistantShaped(e); ok {
		signals = append(signals, signal{
			Kind: "permission-shape",
			Note: "ai signal: permission-shape, " + why,
		})
	}
	return entry, signals
}

// assistantShaped reports the permission combination that only an extension
// driving pages on the user's behalf needs: reach over every site, a way to
// act on the page, and a private channel out of the tab. A password manager or
// an ad blocker has the first two and never the third.
func assistantShaped(e extension) (string, bool) {
	if !hasBroadHost(e.HostPermissions, e.ContentScriptMatches) {
		return "", false
	}
	perms := map[string]bool{}
	for _, p := range e.Permissions {
		perms[p] = true
	}
	canAct := perms["scripting"] || perms["debugger"] || len(e.ContentScriptMatches) > 0
	if !canAct {
		return "", false
	}
	var channel []string
	for _, p := range []string{"sidePanel", "nativeMessaging", "debugger", "tabCapture", "desktopCapture"} {
		if perms[p] {
			channel = append(channel, p)
		}
	}
	if len(channel) == 0 {
		return "", false
	}
	return fmt.Sprintf("access to every site, page scripting, and %s", strings.Join(channel, " + ")), true
}

// signalKinds lists the distinct signal names, for the summary note.
func signalKinds(signals []signal) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range signals {
		if seen[s.Kind] {
			continue
		}
		seen[s.Kind] = true
		out = append(out, s.Kind)
	}
	return out
}
