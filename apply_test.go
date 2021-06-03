package main

import (
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestApply_one_branch_create_feature_branch(t *testing.T) {
	fs := memfs.New()
	store := memory.NewStorage()
	repo, initErr := git.Init(store, fs)
	if !assert.NoError(t, initErr) {
		return
	}

	signature := someSignature()

	wt, wtErr := repo.Worktree()
	if !assert.NoError(t, wtErr) {
		return
	}

	createInitialCommitOnMain(t, wt)
	if !assert.NoError(t, repo.Storer.RemoveReference(plumbing.Master)) {
		return
	}

	for _, dir := range []string{"foo", "bar", "baz"} {
		if !assert.NoError(t, wt.Filesystem.MkdirAll(dir, 0777)) {
			return
		}
		createFile(t, wt.Filesystem, dir+"/main.yml", fmt.Sprintf("---\nlast_char: %c\n", dir[len(dir)-1]))
	}
	if !assert.NoError(t, wt.AddGlob("*/main.yml")) {
		return
	}

	_, commitErr := wt.Commit("add last_char", &git.CommitOptions{Author: &signature, Committer: &signature, All: true})
	if !assert.NoError(t, commitErr) {
		return
	}

	if !assert.NoError(t,
		Apply(repo,
			`.version = "2.0"`,
			"main",
			"*/main.yml", "add version\n\nQuery: {{.Query}}\n", "version-",
			signature,
			testing.Verbose(), false,
		),
	) {
		return
	}

	t.Run("check commit message", func(t *testing.T) {
		versionMainRef, getVersionMainBranchErr := repo.Storer.Reference(plumbing.NewBranchReferenceName("version-main"))
		if !assert.NoError(t, getVersionMainBranchErr) {
			return
		}

		assert.Equal(t, versionMainRef.Type(), plumbing.HashReference)

		commitObj, getCommitObjErr := repo.Storer.EncodedObject(plumbing.CommitObject, versionMainRef.Hash())
		if !assert.NoError(t, getCommitObjErr) {
			return
		}

		assert.Equal(t, commitObj.Type(), plumbing.CommitObject)

		rd, rdErr := commitObj.Reader()
		if !assert.NoError(t, rdErr) {
			return
		}
		commitMessage, readCommitMessageErr := ioutil.ReadAll(rd)
		if !assert.NoError(t, readCommitMessageErr) {
			return
		}

		assert.Contains(t, string(commitMessage), "add version")
		assert.Contains(t, string(commitMessage), `Query: .version = "2.0"`)
	})

	t.Run("ensure worktree files contain change", func(t *testing.T) {
		if !assert.NoError(t, wt.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName("version-main"),
		})) {
			return
		}

		type Data struct {
			LastChar string `yaml:"last_char"`
			Version  string `yaml:"version"`
		}

		for _, fileName := range []string{
			"foo/main.yml",
			"bar/main.yml",
			"baz/main.yml",
		} {
			t.Run(filepath.Dir(fileName), func(t *testing.T) {
				f, openErr := wt.Filesystem.Open(fileName)
				if !assert.NoError(t, openErr) {
					return
				}

				var data Data

				dec := yaml.NewDecoder(f)

				dec.KnownFields(true)

				if !assert.NoError(t, dec.Decode(&data), "parsing main.yml failed") {
					return
				}

				if !assert.NoError(t, f.Close()) {
					return
				}

				assert.NotEmpty(t, data.LastChar)
				assert.Equal(t, "2.0", data.Version)
			})
		}
	})
}

func TestApply_update_existing_branches(t *testing.T) {
	fs := memfs.New()
	store := memory.NewStorage()
	repo, initErr := git.Init(store, fs)
	if !assert.NoError(t, initErr) {
		return
	}

	signature := someSignature()

	wt, wtErr := repo.Worktree()
	if !assert.NoError(t, wtErr) {
		return
	}

	createInitialCommitOnMain(t, wt)
	if !assert.NoError(t, repo.Storer.RemoveReference(plumbing.Master)) {
		return
	}

	for _, dir := range []string{"foo", "bar", "baz"} {
		if !assert.NoError(t, wt.Filesystem.MkdirAll(dir, 0777)) {
			return
		}
		createFile(t, wt.Filesystem, dir+"/main.yml", fmt.Sprintf("---\nlast_char: %c\n", dir[len(dir)-1]))
	}
	if !assert.NoError(t, wt.AddGlob("*/main.yml")) {
		return
	}

	_, commitErr := wt.Commit("add last_char", &git.CommitOptions{Author: &signature, Committer: &signature, All: true})
	if !assert.NoError(t, commitErr) {
		return
	}

	for _, v := range []string{"2.5", "2.6", "2.7"} {
		b := "rel/" + v
		checkoutErr := wt.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(b),
			Create: true,
		})
		if !assert.NoError(t, checkoutErr) {
			return
		}

		if !assert.NoError(t,
			Apply(repo,
				fmt.Sprintf(`.version = %q`, v),
				strings.ReplaceAll(b, ".", "\\."),
				"*/main.yml", "set version\n\nQuery: {{.Query}}\n", "",
				signature,
				testing.Verbose(), true,
			),
		) {
			return
		}
	}

	if !assert.NoError(t,
		Apply(repo,
			`.greeting = "¡Holla!"`,
			defaultBranchRegex.String(),
			"*/main.yml", "set greeting\n\nQuery: {{.Query}}\n", "",
			signature,
			testing.Verbose(), true,
		),
	) {
		return
	}

	branchIter, branchIterErr := repo.Branches()
	if !assert.NoError(t, branchIterErr) {
		return
	}

	branchIterForEachErr := branchIter.ForEach(func(reference *plumbing.Reference) error {
		t.Run(fmt.Sprintf("branch %s", reference.Name().Short()), func(t *testing.T) {
			t.Run("check commit message", func(t *testing.T) {
				ref, getVersionMainBranchErr := repo.Storer.Reference(reference.Name())
				if !assert.NoError(t, getVersionMainBranchErr) {
					return
				}

				assert.Equal(t, ref.Type(), plumbing.HashReference)

				commitObj, getCommitObjErr := repo.Storer.EncodedObject(plumbing.CommitObject, ref.Hash())
				if !assert.NoError(t, getCommitObjErr) {
					return
				}

				assert.Equal(t, commitObj.Type(), plumbing.CommitObject)

				rd, rdErr := commitObj.Reader()
				if !assert.NoError(t, rdErr) {
					return
				}
				commitMessage, readCommitMessageErr := ioutil.ReadAll(rd)
				if !assert.NoError(t, readCommitMessageErr) {
					return
				}

				assert.Contains(t, string(commitMessage), "set greeting")
				assert.Contains(t, string(commitMessage), `Query: .greeting = "¡Holla!"`)
			})

			t.Run("ensure worktree files contain change", func(t *testing.T) {
				if !assert.NoError(t, wt.Checkout(&git.CheckoutOptions{
					Branch: reference.Name(),
				})) {
					return
				}

				type Data struct {
					LastChar string `yaml:"last_char"`
					Version  string `yaml:"version"`
					Greeting string `yaml:"greeting"`
				}

				for _, fileName := range []string{
					"foo/main.yml",
					"bar/main.yml",
					"baz/main.yml",
				} {
					t.Run(filepath.Dir(fileName), func(t *testing.T) {
						f, openErr := wt.Filesystem.Open(fileName)
						if !assert.NoError(t, openErr) {
							return
						}

						var data Data

						dec := yaml.NewDecoder(f)
						dec.KnownFields(true)

						if !assert.NoError(t, dec.Decode(&data), "parsing main.yml failed") {
							return
						}

						if !assert.NoError(t, f.Close()) {
							return
						}

						assert.NotEmpty(t, data.LastChar)
						if reference.Name().Short() != "main" {
							assert.Equal(t, path.Base(reference.Name().Short()), data.Version)
						}
						assert.Equal(t, "¡Holla!", data.Greeting)
					})
				}
			})
		})

		return nil
	})

	assert.NoError(t, branchIterForEachErr)
}

func createFile(t *testing.T, fs billy.Basic, path, contents string) {
	t.Helper()

	f, createErr := fs.Create(path)
	if !assert.NoError(t, createErr) {
		return
	}
	defer func() {
		if !assert.NoError(t, f.Close()) {
			return
		}
	}()

	_, writeErr := f.Write([]byte(contents))
	if !assert.NoError(t, writeErr) {
		return
	}
}

func createInitialCommitOnMain(t *testing.T, wt *git.Worktree) {
	t.Helper()

	signature := someSignature()

	keepFile, createFileErr := wt.Filesystem.Create(".git-keep")
	if !assert.NoError(t, createFileErr) {
		return
	}

	if !assert.NoError(t, keepFile.Close()) {
		return
	}

	_, addErr := wt.Add(".git-keep")
	if !assert.NoError(t, addErr) {
		return
	}

	_, commitErr := wt.Commit("initial commit", &git.CommitOptions{Author: &signature, Committer: &signature})
	if !assert.NoError(t, commitErr) {
		return
	}

	checkoutErr := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("main"),
		Create: true,
	})

	if !assert.NoError(t, checkoutErr) {
		return
	}
}

func someSignature() object.Signature {
	return object.Signature{
		Name:  "christopher",
		Email: "christopher@exmaple.com",
		When:  time.Unix(1622680178, 0),
	}
}
