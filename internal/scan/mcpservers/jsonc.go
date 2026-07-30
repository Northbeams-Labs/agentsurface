package mcpservers

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Several of the clients here ship their config through an editor that accepts
// JSONC: // and /* */ comments and a trailing comma before a closing brace.
// VS Code writes it, Zed writes it, and a user who hand-edits any of the others
// will eventually write it too. encoding/json rejects all of that, so a strict
// parse would report "no servers" on a machine that has plenty. Rather than
// guess, strip the parts encoding/json cannot read and keep everything else
// byte for byte.

// decodeJSONC unmarshals JSON that may carry comments or trailing commas.
func decodeJSONC(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err == nil {
		return nil
	}
	cleaned := stripJSONC(data)
	if err := json.Unmarshal(cleaned, v); err != nil {
		return fmt.Errorf("not valid JSON, even after comments and trailing commas were removed: %w", err)
	}
	return nil
}

// stripJSONC removes comments and trailing commas outside of string literals.
// Comment bodies are replaced with spaces rather than deleted so that byte
// offsets in any error message still line up with the file on disk.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	for i := 0; i < len(data); i++ {
		c := data[i]

		if inString {
			out = append(out, c)
			switch c {
			case '\\':
				if i+1 < len(data) {
					i++
					out = append(out, data[i])
				}
			case '"':
				inString = false
			}
			continue
		}

		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				if data[i] == '\n' {
					out = append(out, '\n')
				}
				i++
			}
			i++ // land on the '/' of the closing pair
		default:
			out = append(out, c)
		}
	}
	return removeTrailingCommas(out)
}

// removeTrailingCommas deletes a comma that is followed only by whitespace and
// then a } or ]. Comments are already gone by the time this runs.
func removeTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	for i := 0; i < len(data); i++ {
		c := data[i]

		if inString {
			out = append(out, c)
			switch c {
			case '\\':
				if i+1 < len(data) {
					i++
					out = append(out, data[i])
				}
			case '"':
				inString = false
			}
			continue
		}

		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}

		if c == ',' {
			rest := bytes.TrimLeft(data[i+1:], " \t\r\n")
			if len(rest) > 0 && (rest[0] == '}' || rest[0] == ']') {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}
