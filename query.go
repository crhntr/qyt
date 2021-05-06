package main

import (
	"bytes"
	"container/list"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"github.com/sergi/go-diff/diffmatchpatch"
	"gopkg.in/yaml.v3"
)

type CommitMessageData struct {
	Branch string
	Query  string
}

func Query(repo *git.Repository, exp *yqlib.ExpressionNode, branches []plumbing.Reference, verbose, forceCheckout bool, filePattern string) error {
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("could not open worktree: %w", err)
	}

	for _, branch := range branches {
		if verbose {
			fmt.Printf("# \tchecking out %q\n", branch.Name().Short())
		}

		err = wt.Checkout(&git.CheckoutOptions{
			Branch: branch.Name(),
			Force:  forceCheckout,
		})
		if err != nil {
			return fmt.Errorf("could not checkout %s: %w", branch.Name().Short(), err)
		}

		err = Walk(wt.Filesystem, "", func(path string, info fs.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}
			if matched, err := filepath.Match(filePattern, path); err != nil {
				return err
			} else if !matched {
				return nil
			}

			f, err := wt.Filesystem.Open(path)
			if err != nil {
				return fmt.Errorf("could not open file %q: %s", path, err)
			}

			if verbose {
				fmt.Printf("# \t\tapplying yq operation to file %q\n", path)
			}

			var buf bytes.Buffer

			err = applyExpression(&buf, f, exp, path, map[string]string{
				"branch": branch.Name().Short(),
			})
			if err != nil {
				return fmt.Errorf("could not apply yq operation to file %q: %s", path, err)
			}
			fmt.Print(buf.String())

			return nil
		})
		if err != nil {
			return fmt.Errorf("failed while waliking files on %s: %w", branch.Name().Short(), err)
		}

		fmt.Println()
	}
	return nil
}

func Apply(repo *git.Repository, exp *yqlib.ExpressionNode, branches []plumbing.Reference, verbose, forceCheckout bool, filePattern, msg, expString string, confirmCommit func() bool) error {
	commitTemplate, err := template.New("").Parse(msg)
	if err != nil {
		return fmt.Errorf("could not parse commit message template: %w", err)
	}
	conf, err := repo.ConfigScoped(config.SystemScope)
	if err != nil {
		return fmt.Errorf("could not get git config: %w", err)
	}
	if conf.User.Name == "" {
		return fmt.Errorf("git user name not set in config")
	}
	if conf.User.Email == "" {
		return fmt.Errorf("git user email not set in config")
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("could not open worktree: %w", err)
	}

	for _, branch := range branches {
		if verbose {
			fmt.Printf("# \tchecking out %q\n", branch.Name().Short())
		}
		err = wt.Checkout(&git.CheckoutOptions{
			Branch: branch.Name(),
			Force:  forceCheckout,
		})
		if err != nil {
			return fmt.Errorf("could not checkout %s: %w", branch.Name().Short(), err)
		}

		dmp := diffmatchpatch.New()

		err = Walk(wt.Filesystem, "", func(path string, info fs.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}

			if matched, err := filepath.Match(filePattern, path); err != nil {
				return err
			} else if !matched {
				return nil
			}
			if verbose {
				fmt.Printf("# \t\topening file %q\n", path)
			}

			f, err := wt.Filesystem.Open(path)
			if err != nil {
				return fmt.Errorf("could not open file %q: %s", path, err)
			}
			defer func() {
				_ = f.Close()
			}()

			in, err := ioutil.ReadAll(f)
			if err != nil {
				return fmt.Errorf("could not read file %q: %s", path, err)
			}

			var out bytes.Buffer

			err = applyExpression(&out, bytes.NewReader(in), exp, path, map[string]string{
				"branch": branch.Name().Short(),
			})
			if err != nil {
				return fmt.Errorf("could not apply yq operation to file %q: %s", path, err)
			}

			wf, err := wt.Filesystem.Create(path)
			if err != nil {
				return fmt.Errorf("could not open file for writing %q: %s", path, err)
			}
			outStr := out.String()
			_, err = io.Copy(wf, strings.NewReader(outStr))
			if err != nil {
				return fmt.Errorf("could not write query result to file %q on branch %s: %s", path, branch.Name().Short(), err)
			}

			diffs := dmp.DiffMain(string(in), outStr, false)
			_, err = wt.Add(path)
			if err != nil {
				return fmt.Errorf("could not add file %q: %s", path, err)
			}

			fmt.Printf("diff %s\n\n%s\n", path, dmp.DiffPrettyText(diffs))

			return nil
		})
		if err != nil {
			return fmt.Errorf("failed while waliking files on %s: %w", branch.Name().Short(), err)
		}

		status, err := wt.Status()
		if err != nil {
			return fmt.Errorf("failed to get git status for %q: %s", branch.Name().Short(), err)
		}
		fmt.Printf("On branch %s\nChanges to be committed:\n", branch.Name().Short())
		fmt.Println(status.String())

		if status.IsClean() {
			continue
		}

		var buf bytes.Buffer
		err = commitTemplate.Execute(&buf, CommitMessageData{
			Branch: branch.Name().Short(),
			Query:  expString,
		})
		if err != nil {
			return fmt.Errorf("failed to generate commit message for branch %s: %w", branch.Name().Short(), err)
		}

		fmt.Printf("Commit message:\n%s\n", buf.String())

		if confirmCommit() {
			now := time.Now()
			hash, err := wt.Commit(buf.String(), &git.CommitOptions{
				Author: &object.Signature{
					Name:  conf.User.Name,
					Email: conf.User.Email,
					When:  now,
				},
				Committer: &object.Signature{
					Name:  conf.User.Name,
					Email: conf.User.Email,
					When:  now,
				},
			})
			if err != nil {
				return fmt.Errorf("failed to write commit for branch %s: %w", branch.Name().Short(), err)
			}
			fmt.Printf("Successfully committed %s on branch %s\n", hash.String(), branch.Name().Short())
		}
	}

	return nil
}

func applyExpression(w io.Writer, r io.Reader, exp *yqlib.ExpressionNode, filename string, variables map[string]string) error {
	var bucket yaml.Node
	decoder := yaml.NewDecoder(r)
	err := decoder.Decode(&bucket)
	if err != nil {
		return fmt.Errorf("failed to decode yaml: %s", err)
	}

	navigator := yqlib.NewDataTreeNavigator()

	nodes := list.New()
	nodes.PushBack(&yqlib.CandidateNode{
		Filename:         filename,
		Node:             &bucket,
		EvaluateTogether: true,
	})

	ctx := yqlib.Context{
		MatchingNodes: nodes,
	}
	for k, v := range variables {
		ctx.SetVariable(k, scopeVariable(v))
	}

	result, err := navigator.GetMatchingNodes(ctx, exp)
	if err != nil {
		return fmt.Errorf("yq operation failed: %w", err)
	}

	printer := yqlib.NewPrinter(w, false, false, false, 2, true)

	err = printer.PrintResults(result.MatchingNodes)
	if err != nil {
		return fmt.Errorf("rendering result failed: %w", err)
	}

	return nil
}

func scopeVariable(value string) *list.List {
	nodes := list.New()

	var bucket yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(fmt.Sprintf("%q", value)))
	err := decoder.Decode(&bucket)
	if err != nil {
		panic(fmt.Sprintf("failed to decode yaml: %s", err))
	}
	nodes.PushBack(&yqlib.CandidateNode{
		Node: &bucket,
	})

	return nodes
}

func Walk(fs billy.Filesystem, root string, walkFn filepath.WalkFunc) error {
	info, err := fs.Lstat(root)
	if err != nil {
		err = walkFn(root, nil, err)
	} else {
		err = walk(fs, root, info, walkFn)
	}
	if err == filepath.SkipDir {
		return nil
	}
	return err
}

func walk(fs billy.Filesystem, path string, info os.FileInfo, walkFn filepath.WalkFunc) error {
	if !info.IsDir() {
		return walkFn(path, info, nil)
	}

	names, err := readDirNames(fs, path)
	err1 := walkFn(path, info, err)
	// If err != nil, walk can't walk into this directory.
	// err1 != nil means walkFn want walk to skip this directory or stop walking.
	// Therefore, if one of err and err1 isn't nil, walk will return.
	if err != nil || err1 != nil {
		// The caller's behavior is controlled by the return value, which is decided
		// by walkFn. walkFn may ignore err and return nil.
		// If walkFn returns SkipDir, it will be handled by the caller.
		// So walk should return whatever walkFn returns.
		return err1
	}

	for _, name := range names {
		filename := filepath.Join(path, name)
		fileInfo, err := fs.Lstat(filename)
		if err != nil {
			if err := walkFn(filename, fileInfo, err); err != nil && err != filepath.SkipDir {
				return err
			}
		} else {
			err = walk(fs, filename, fileInfo, walkFn)
			if err != nil {
				if !fileInfo.IsDir() || err != filepath.SkipDir {
					return err
				}
			}
		}
	}

	return nil
}

func readDirNames(fs billy.Filesystem, dirname string) ([]string, error) {
	infos, err := fs.ReadDir(dirname)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, info := range infos {
		names = append(names, info.Name())
	}
	sort.Strings(names)
	return names, nil
}
