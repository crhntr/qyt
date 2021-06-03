package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/assert"
)

func TestQuery(t *testing.T) {
	fs := memfs.New()
	store := memory.NewStorage()
	repo, initErr := git.Init(store, fs)
	assert.NoError(t, initErr)

	createSomeFilesWithNameKey(t, repo, "", "foo")
	createSomeFilesWithNameKey(t, repo, "b", "bar", "baz")

	var out bytes.Buffer
	queryErr := Query(&out, repo, `{"n": .name, "b": $branch, "f": $filename}`, ".*", "*.yml", false, true)
	assert.NoError(t, queryErr)

	dec := json.NewDecoder(&out)
	// got has strings: map[branch][filename]name
	got := make(map[string]map[string]string)
	for {
		var m map[string]string
		decodeErr := dec.Decode(&m)
		if decodeErr != nil {
			break
		}
		fileAndContents := got[m["b"]]
		if fileAndContents == nil {
			fileAndContents = make(map[string]string)
		}
		fileAndContents[m["f"]] = m["n"]
		got[m["b"]] = fileAndContents
	}

	expected := map[string]map[string]string{
		"b":      {"bar.yml": "about bar", "baz.yml": "about baz", "foo.yml": "about foo"},
		"master": {"foo.yml": "about foo"},
	}

	assert.Equal(t, got, expected)
}

func createSomeFilesWithNameKey(t *testing.T, repo *git.Repository, branch string, names ...string) {
	t.Helper()

	wt, wtErr := repo.Worktree()
	assert.NoError(t, wtErr)

	sig := someSignature()

	if branch != "" {
		checkoutErr := wt.Checkout(&git.CheckoutOptions{Create: true, Branch: plumbing.NewBranchReferenceName(branch)})
		assert.NoError(t, checkoutErr)
	}

	for _, name := range names {
		p := name + ".yml"
		fooTxt, createFooErr := wt.Filesystem.Create(p)
		assert.NoError(t, createFooErr)
		_, printErr := fmt.Fprintf(fooTxt, "---\nname: about %s\n", name)
		assert.NoError(t, printErr)

		_ = fooTxt.Close()

		_, addErr := wt.Add(p)
		assert.NoError(t, addErr)

		_, commitErr := wt.Commit(fmt.Sprintf("add %s", name), &git.CommitOptions{Author: &sig, Committer: &sig})
		assert.NoError(t, commitErr)
	}
}
