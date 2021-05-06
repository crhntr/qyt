package main

import (
	"bufio"
	_ "embed"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	markdown "github.com/MichaelMure/go-term-markdown"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"gopkg.in/op/go-logging.v1"
)

var defaultBranchRegex = regexp.MustCompile(`(main)|(rel/\d+\.\d+)`)

//go:embed README.md
var README string

func main() {
	var (
		repoPath,
		branchesPattern,
		commitMessage string
		forceCheckout, verbose, noConfirm bool
	)

	backend := logging.NewLogBackend(ioutil.Discard, "", 0)
	logging.SetBackend(backend)

	flags := &flag.FlagSet{}
	flags.StringVar(&branchesPattern, "b", defaultBranchRegex.String(), "regular expression for branches")
	flags.StringVar(&repoPath, "r", ".", "path to local git repository")

	flags.StringVar(&commitMessage, "m", "", "commit message template (CommitMessageData is passed when executing the template)")
	flags.BoolVar(&forceCheckout, "f", false, "force checkout (will throw away local changes)")
	flags.BoolVar(&verbose, "v", false, "verbose logging")
	flags.BoolVar(&noConfirm, "no-confirm", false, "skip commit confirmation")
	flags.Usage = func() {
		fmt.Print(string(markdown.Render(README+"\n## Options", 80, 0)))
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

	repo, err := git.PlainOpen(repoPath)
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

	input := bufio.NewReader(os.Stdin)
	if commitMessage != "" {
		err = Apply(repo, yqExpression, branches, verbose, forceCheckout, filePattern, commitMessage, yqExpressionString, func() bool {
			if noConfirm {
				return true
			}
			for {
				fmt.Printf("Commit [Y,n]? ")
				text, readErr := input.ReadString('\n')
				if readErr != nil {
					continue
				}
				return strings.TrimSpace(text) == "Y"
			}
		})
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		return
	}

	err = Query(repo, yqExpression, branches, verbose, forceCheckout, filePattern)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
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
