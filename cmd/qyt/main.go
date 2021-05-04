package main

import (
	"flag"
	"fmt"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"gopkg.in/op/go-logging.v1"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-git/go-git/v5"

	"github.com/crhntr/qyt"
)

var defaultBranchRegex = regexp.MustCompile(`(main)|(rel/\d+\.\d+)`)

func main() {
	var (
		repoPath,
		branchesPattern,
		commitMessage string
		forceCheckout, verbose bool
	)

	backend := logging.NewLogBackend(ioutil.Discard, "", 0)
	logging.SetBackend(backend)

	flags := &flag.FlagSet{}
	flags.StringVar(&branchesPattern, "b", defaultBranchRegex.String(), "regular expression for branches")
	flags.StringVar(&repoPath, "r", ".", "path to local git repository")

	flags.StringVar(&commitMessage, "m", "", "commit message template (qyt.CommitMessageData is passed when executing the template)")
	flags.BoolVar(&forceCheckout, "f", false, "force checkout (will throw away local changes)")
	flags.BoolVar(&verbose, "v", false, "verbose logging")
	flags.Usage = func() {
		fmt.Printf("usage: %s [options] <yq_expression> <file pattern>\n", os.Args[0])
		flags.PrintDefaults()
	}

	err := flags.Parse(os.Args[1:])
	if err != nil {
		fmt.Println("could not parse flags", err)
		flags.Usage()
		os.Exit(1)
	}

	if flags.NArg() < 2 {
		fmt.Println("missing required argument(s)")
		flags.Usage()
		os.Exit(1)
	}
	yqExpressionString := flags.Arg(0)
	filePattern := flags.Arg(1)

	repo, err := openRepo(repoPath, memfs.New())
	if err != nil {
		fmt.Println("failed to open repository", err)
		os.Exit(1)
	}

	parser := yqlib.NewExpressionParser()
	yqExpression, err := parser.ParseExpression(yqExpressionString)
	if err != nil {
		fmt.Printf("failed to parse yq expression: %s\n", err)
		os.Exit(1)
	}

	branches, err := branchesMatchingRegex(branchesPattern, repo, verbose)
	if err != nil {
		fmt.Printf("failed to match branches: %s\n", err)
		os.Exit(1)
	}

	if commitMessage != "" {
		err = qyt.Apply(repo, yqExpression, branches, verbose, filePattern, commitMessage, yqExpressionString)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		return
	}

	err = qyt.Query(repo, yqExpression, branches, verbose, filePattern)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func openRepo(repoPath string, fs billy.Filesystem) (*git.Repository, error) {
	absRepoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, err
	}
	dotGitInfo, err := os.Stat(filepath.Join(absRepoPath, git.GitDirName))
	if err != nil {
		return nil, err
	}
	if !dotGitInfo.IsDir() {
		return nil, err
	}
	dotGit := osfs.New(filepath.Join(repoPath, git.GitDirName))
	c := filesystem.NewStorage(dotGit, cache.NewObjectLRU(cache.DefaultMaxSize))
	return git.Open(c, fs)
}

func branchesMatchingRegex(branchPattern string, repo *git.Repository, verbose bool) ([]plumbing.Reference, error) {
	var branches []plumbing.Reference

	branchExp, err := regexp.Compile(branchPattern)
	if err != nil {
		return nil, fmt.Errorf("could not complile branch regular expression: %w", err)
	}
	branchIter, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("faild to get branch iterator: %w", err)
	}
	_ = branchIter.ForEach(func(reference *plumbing.Reference) error {
		if branchExp.MatchString(reference.Name().Short()) {
			branches = append(branches, *reference)
		}
		return nil
	})

	if verbose {
		if len(branches) == 1 {
			fmt.Printf("# 1 branch matches regular expression %q\n", branchPattern)
		} else {
			fmt.Printf("# %d branches match regular expression %q\n", len(branches), branchPattern)
		}
	}

	return branches, nil
}