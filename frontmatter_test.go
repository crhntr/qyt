package qyt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name            string
		source          string
		wantFrontmatter string
		wantBody        string
		wantOK          bool
	}{
		{
			name:            "happy path",
			source:          "---\ntitle: Hello World\ndate: 2024-01-01\n---\n# Hello World\nThis is the body.\n",
			wantFrontmatter: "title: Hello World\ndate: 2024-01-01\n",
			wantBody:        "# Hello World\nThis is the body.\n",
			wantOK:          true,
		},
		{
			name:   "no frontmatter",
			source: "# Just a heading\nNo frontmatter here.\n",
			wantOK: false,
		},
		{
			name:   "invalid YAML frontmatter",
			source: "---\ninvalid: [yaml: {broken\n---\n# Body\n",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotFM, gotBody, gotOK := SplitFrontmatter([]byte(tc.source))
			assert.Equal(t, tc.wantOK, gotOK)
			if tc.wantOK {
				assert.Equal(t, tc.wantFrontmatter, string(gotFM))
				assert.Equal(t, tc.wantBody, string(gotBody))
			} else {
				assert.Equal(t, tc.source, string(gotBody))
			}
		})
	}
}

func TestIsMarkdownFile(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		{filename: "file.md", want: true},
		{filename: "file.markdown", want: true},
		{filename: "file.yml", want: false},
		{filename: "file.yaml", want: false},
		{filename: "file.txt", want: false},
		{filename: "noextension", want: false},
		{filename: "file.MD", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.filename, func(t *testing.T) {
			assert.Equal(t, tc.want, IsMarkdownFile(tc.filename))
		})
	}
}
