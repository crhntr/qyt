package qyt

import (
	"bytes"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

// SplitFrontmatter extracts YAML frontmatter from markdown source.
// Frontmatter is a YAML block delimited by --- lines at the very start of the document.
// The YAML content is validated with go.yaml.in/yaml/v4.
// It returns the raw frontmatter YAML bytes, the markdown body after the closing ---,
// and whether valid frontmatter was found.
func SplitFrontmatter(source []byte) (frontmatter, body []byte, ok bool) {
	const sep = "---"

	// Must start with exactly "---\n" or "---\r\n"
	rest := source
	if !bytes.HasPrefix(rest, []byte(sep+"\n")) && !bytes.HasPrefix(rest, []byte(sep+"\r\n")) {
		return nil, source, false
	}

	// Advance past the opening ---
	rest = rest[len(sep):]
	if rest[0] == '\r' {
		rest = rest[1:]
	}
	rest = rest[1:] // consume \n

	// Find closing ---
	for {
		idx := bytes.Index(rest, []byte("\n"+sep))
		if idx < 0 {
			// No closing ---, not valid frontmatter
			return nil, source, false
		}
		// idx is the position of the \n before ---
		// Candidate close: rest[idx+1 : idx+1+len(sep)]
		after := rest[idx+1+len(sep):]
		if len(after) == 0 || after[0] == '\n' || after[0] == '\r' {
			// Validate YAML
			fmBytes := rest[:idx+1] // frontmatter content (including trailing newline)
			var dummy any
			if err := yaml.Unmarshal(fmBytes, &dummy); err != nil {
				return nil, source, false
			}
			// Advance past closing --- and optional \r\n or \n
			bodyStart := after
			if len(bodyStart) > 0 && bodyStart[0] == '\r' {
				bodyStart = bodyStart[1:]
			}
			if len(bodyStart) > 0 && bodyStart[0] == '\n' {
				bodyStart = bodyStart[1:]
			}
			return fmBytes, bodyStart, true
		}
		// Not a valid close (e.g. --- followed by other chars), keep searching
		rest = rest[idx+1:]
	}
}

// IsMarkdownFile reports whether filename is a markdown file by extension.
func IsMarkdownFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".md" || ext == ".markdown"
}
