package qyt

import (
	"bytes"
	"container/list"
	"fmt"
	"io"
	"io/fs"
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
	"gopkg.in/yaml.v3"
)

type CommitMessageData struct {
	Branch plumbing.ReferenceName
	Query  string
}

func Query(repo *git.Repository, exp *yqlib.ExpressionNode, branches []plumbing.Reference, verbose bool, filePattern string) error {
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
			Force:  true,
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

			err = applyExpression(&buf, f, exp)
			if err != nil {
				return fmt.Errorf("could not apply yq operation to file %q: %s", path, err)
			}

			result := strings.TrimSpace(buf.String())
			if result != "" {
				fmt.Println(result)
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("failed while waliking files on %s: %w", branch.Name().Short(), err)
		}
	}
	return nil
}

func Apply(repo *git.Repository, exp *yqlib.ExpressionNode, branches []plumbing.Reference, verbose bool, filePattern, msg, expString string) error {
	commitTemplate, err := template.New("").Parse(msg)
	if err != nil {
		return fmt.Errorf("could not parse commit message template: %w", err)
	}
	conf, err := repo.ConfigScoped(config.SystemScope)
	if err != nil {
		return fmt.Errorf("could not get git config: %w", err)
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
			Force:  true,
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
			if verbose {
				fmt.Printf("# \t\topening file %q\n", path)
			}

			f, err := wt.Filesystem.Open(path)
			if err != nil {
				return fmt.Errorf("could not open file %q: %s", path, err)
			}

			var buf bytes.Buffer

			err = applyExpression(&buf, f, exp)
			if err != nil {
				return fmt.Errorf("could not apply yq operation to file %q: %s", path, err)
			}

			err = f.Truncate(0)
			if err != nil {
				return fmt.Errorf("could not update file %q: %s", path, err)
			}
			_, err = io.Copy(f, &buf)
			if err != nil {
				return fmt.Errorf("could not update file %q: %s", path, err)
			}

			_, err = wt.Add(path)
			if err != nil {
				return fmt.Errorf("could not add file %q: %s", path, err)
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("failed while waliking files on %s: %w", branch.Name().Short(), err)
		}

		status, err := wt.Status()
		if err != nil {
			return fmt.Errorf("failed to get git status for %q: %s", branch.Name().Short(), err)
		}
		fmt.Println(status.String())

		if status.IsClean() {
			continue
		}

		var buf bytes.Buffer
		err = commitTemplate.Execute(&buf, CommitMessageData{
			Branch: branch.Name(),
			Query:  expString,
		})
		if err != nil {
			return fmt.Errorf("failed to generate commit message for branch %s: %w", branch.Name().Short(), err)
		}

		now := time.Now()
		_, err = wt.Commit(buf.String(), &git.CommitOptions{
			Author: &object.Signature{
				Name:  conf.Author.Name,
				Email: conf.Author.Email,
				When:  now,
			},
			Committer: &object.Signature{
				Name:  conf.Author.Name,
				Email: conf.Author.Email,
				When:  now,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to write commit for branch %s: %w", branch.Name().Short(), err)
		}
	}

	return nil
}

func applyExpression(w io.Writer, r io.Reader, exp *yqlib.ExpressionNode) error {
	var bucket yaml.Node
	decoder := yaml.NewDecoder(r)
	err := decoder.Decode(&bucket)
	if err != nil {
		return fmt.Errorf("failed to decode yaml: %s", err)
	}

	navigator := yqlib.NewDataTreeNavigator()

	nodes := list.New()
	nodes.PushBack(&yqlib.CandidateNode{
		Filename:         "in.yml",
		Node:             &bucket,
		FileIndex:        0,
		EvaluateTogether: true,
	})

	result, err := navigator.GetMatchingNodes(yqlib.Context{MatchingNodes: nodes}, exp)
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
