package main

import (
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestTreeView(t *testing.T) {
	repo, err := git.PlainOpen("../../config")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatal(err)
	}
	tree, err := repo.TreeObject(c.TreeHash)
	if err != nil {
		t.Fatal(err)
	}

	root := parseNodes(tree)

	t.Logf("%v", root)

	n, ok := root.find("a/b")
	if !ok {
		t.Fail()
	}
	t.Logf("%v\t%t", n, ok)

	n, ok = root.find("a/b/c/d/file.yml")
	if !ok {
		t.Fail()
	}
	t.Logf("%v\t%t", n, ok)

	n, ok = root.find("a")
	if !ok {
		t.Fail()
	}
	t.Logf("%v\t%t", n, ok)
}
