package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/op/go-logging.v1"

	"github.com/crhntr/qyt"
)

func init() {
	backend := logging.NewLogBackend(io.Discard, "", 0)
	logging.SetBackend(backend)
}

func main() {
	allowOverridingExistingBranches := false
	flag.BoolVar(&allowOverridingExistingBranches, "allow-overriding-existing-branches", false, "Allow overriding existing branches")
	flag.Parse()

	qytConfig, usage, err := qyt.LoadConfiguration(flag.Args()[1:])
	if err != nil {
		usage()
		os.Exit(1)
	}
	repo, err := git.PlainOpen(qytConfig.GitRepositoryPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", err)
		usage()
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "query":
		err = qyt.Query(os.Stdout, repo, qytConfig.Query, qytConfig.BranchFilter, qytConfig.FileNameFilter, false, false)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "query error: %s\n", err.Error())
			os.Exit(1)
		}
	case "apply":
		author, getSignatureErr := getSignature(repo, time.Now())
		if getSignatureErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", getSignatureErr)
			os.Exit(1)
		}

		err = qyt.Apply(repo, qytConfig.Query, qytConfig.BranchFilter, qytConfig.FileNameFilter, qytConfig.CommitTemplate, qytConfig.NewBranchPrefix, author, false, allowOverridingExistingBranches)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "apply error: %s\n", err.Error())
			os.Exit(1)
		}
		return
	}
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
