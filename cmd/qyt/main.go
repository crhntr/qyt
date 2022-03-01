package main

import (
	"bufio"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	markdown "github.com/MichaelMure/go-term-markdown"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/op/go-logging.v1"

	"github.com/crhntr/qyt"
)

func init() {
	backend := logging.NewLogBackend(io.Discard, "", 0)
	logging.SetBackend(backend)
}

//go:embed README.md
var readme string

func main() {
	var (
		repoPath,
		branchesPattern,
		branchPrefix,
		commitMessage string
		allowOverridingExistingBranches, verbose, outputToJSON bool
		// noConfirm bool
	)

	flags := &flag.FlagSet{}
	flags.StringVar(&branchesPattern, "b", qyt.DefaultBranchRegex().String(), "regular expression for branches")
	flags.StringVar(&repoPath, "r", ".", "path to local git repository")

	flags.StringVar(&branchPrefix, "p", "", "prefix for created branches (recommended when -m is set)")
	flags.StringVar(&commitMessage, "m", "", "commit message template (CommitMessageData is passed when executing the template)")

	flags.BoolVar(&allowOverridingExistingBranches, "c", false, "commit changes onto existing branches")
	flags.BoolVar(&verbose, "v", false, "verbose logging")

	flags.BoolVar(&outputToJSON, "json", false, "format output as json")
	// flags.BoolVar(&noConfirm, "no-confirm", false, "skip commit confirmation")

	flags.Usage = func() {
		fmt.Print(string(markdown.Render(readme+"\n## Options", 80, 0)))
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
		_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", err)
		os.Exit(1)
	}

	if commitMessage != "" {
		author, getSignatureErr := getSignature(repo, time.Now())
		if getSignatureErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", getSignatureErr)
			os.Exit(1)
		}

		err = qyt.Apply(repo, yqExpressionString, branchesPattern, filePattern, commitMessage, branchPrefix, author, verbose, allowOverridingExistingBranches)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "apply error: %s\n", err.Error())
			os.Exit(1)
		}
		return
	}

	err = qyt.Query(os.Stdout, repo, yqExpressionString, branchesPattern, filePattern, verbose, outputToJSON)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "query error: %s\n", err.Error())
		os.Exit(1)
	}
}

func logCommitMessage(branch plumbing.Reference, obj plumbing.MemoryObject, prefix string) error {
	var commit object.Commit
	commitDecodeErr := commit.Decode(&obj)
	if commitDecodeErr != nil {
		return commitDecodeErr
	}

	fmt.Printf(prefix+"Commiting changes to %s\n", branch.Name().Short())
	r := bufio.NewReader(strings.NewReader(commit.String()))
	for {
		line, readErr := r.ReadString('\n')
		if readErr != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fmt.Printf(prefix+"\t%s\n", line)
	}

	return nil
}

func getSignature(repo *git.Repository, now time.Time) (object.Signature, error) {
	conf, err := repo.ConfigScoped(config.SystemScope)
	if err != nil {
		return object.Signature{}, fmt.Errorf("could not get git config: %w", err)
	}
	if conf.User.Name == "" {
		return object.Signature{}, fmt.Errorf("git user name not set in config")
	}
	if conf.User.Email == "" {
		return object.Signature{}, fmt.Errorf("git user email not set in config")
	}
	return object.Signature{
		Name:  conf.User.Name,
		Email: conf.User.Email,
		When:  now,
	}, nil
}
