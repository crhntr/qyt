package qyt

import (
	"bytes"
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
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

// TestQuery_markdown tests that Query on .md files outputs the frontmatter expression result.
func TestQuery_markdown(t *testing.T) {
	repo, wt := newMemRepo(t)
	sig := someSignature()

	createInitialCommitOnMain(t, wt)
	assert.NoError(t, repo.Storer.RemoveReference(plumbing.Master))

	content := "---\ntitle: Hello World\ndate: 2024-01-01\n---\n# Hello World\nThis is the body.\n"
	createFile(t, wt.Filesystem, "post.md", content)
	_, addErr := wt.Add("post.md")
	assert.NoError(t, addErr)
	_, commitErr := wt.Commit("add post.md", &git.CommitOptions{Author: &sig, Committer: &sig})
	assert.NoError(t, commitErr)

	var out bytes.Buffer
	queryErr := Query(&out, repo, `.title`, `main`, `.*\.md`, false, false)
	assert.NoError(t, queryErr)

	result := out.String()
	assert.Contains(t, result, "Hello World")
	assert.NotContains(t, result, "# Hello World")
	assert.NotContains(t, result, "This is the body")
}

// TestApply_markdown_updates_frontmatter_preserves_body tests that Apply on .md files
// updates the frontmatter and preserves the body.
func TestApply_markdown_updates_frontmatter_preserves_body(t *testing.T) {
	repo, wt := newMemRepo(t)
	sig := someSignature()

	createInitialCommitOnMain(t, wt)
	assert.NoError(t, repo.Storer.RemoveReference(plumbing.Master))

	originalContent := "---\ntitle: Hello\n---\n# Body stays\n"
	createFile(t, wt.Filesystem, "post.md", originalContent)
	_, addErr := wt.Add("post.md")
	assert.NoError(t, addErr)
	_, commitErr := wt.Commit("add post.md", &git.CommitOptions{Author: &sig, Committer: &sig})
	assert.NoError(t, commitErr)

	applyErr := Apply(repo, `.title = "Updated"`, `main`, `.*\.md`, "update title", "updated-", sig, false, false)
	assert.NoError(t, applyErr)

	updatedRef, refErr := repo.Storer.Reference(plumbing.NewBranchReferenceName("updated-main"))
	if !assert.NoError(t, refErr) {
		return
	}

	checkoutErr := wt.Checkout(&git.CheckoutOptions{Branch: updatedRef.Name()})
	if !assert.NoError(t, checkoutErr) {
		return
	}

	f, openErr := wt.Filesystem.Open("post.md")
	if !assert.NoError(t, openErr) {
		return
	}
	defer func() { _ = f.Close() }()

	var buf bytes.Buffer
	_, copyErr := buf.ReadFrom(f)
	assert.NoError(t, copyErr)

	updatedContent := buf.String()

	// Validate structure: only frontmatter changed, body is identical
	fm, body, ok := SplitFrontmatter([]byte(updatedContent))
	assert.True(t, ok)
	assert.Equal(t, "# Body stays\n", string(body))

	var data map[string]any
	assert.NoError(t, yaml.Unmarshal(fm, &data))
	assert.Equal(t, "Updated", data["title"])
}

// TestApply_markdown_no_frontmatter_no_commit tests that Apply on .md with no frontmatter
// does not create a commit.
func TestApply_markdown_no_frontmatter_no_commit(t *testing.T) {
	repo, wt := newMemRepo(t)
	sig := someSignature()

	createInitialCommitOnMain(t, wt)

	// MD file with no frontmatter
	createFile(t, wt.Filesystem, "readme.md", "# Just a heading\nNo frontmatter here.\n")
	_, addErr := wt.Add("readme.md")
	assert.NoError(t, addErr)
	_, commitErr := wt.Commit("add readme.md", &git.CommitOptions{Author: &sig, Committer: &sig})
	assert.NoError(t, commitErr)

	applyErr := Apply(repo, `.title = "Updated"`, `main`, `.*\.md`, "update title", "nofm-", sig, false, false)
	assert.NoError(t, applyErr)

	// No branch should have been created
	_, refErr := repo.Storer.Reference(plumbing.NewBranchReferenceName("nofm-main"))
	assert.Error(t, refErr, "expected no branch to be created when no frontmatter exists")
}

func newMemRepo(t *testing.T) (*git.Repository, *git.Worktree) {
	t.Helper()
	fs := memfs.New()
	repo, initErr := git.Init(memory.NewStorage(), fs)
	if !assert.NoError(t, initErr) {
		t.FailNow()
	}
	wt, wtErr := repo.Worktree()
	if !assert.NoError(t, wtErr) {
		t.FailNow()
	}
	return repo, wt
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
