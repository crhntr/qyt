package main

import (
	"embed"
	"io"
	"os"

	"github.com/crhntr/qyt"
	"github.com/crhntr/qyt/cmd/qyt-webapp/models"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/webview/webview"
	"gopkg.in/op/go-logging.v1"
)

//go:embed embed
var dir embed.FS

func init() {
	backend := logging.NewLogBackend(io.Discard, "", 0)
	logging.SetLevel(logging.CRITICAL, "github.com/mikefarah/yq/v4")
	logging.SetBackend(backend)
}

func main() {
	config, usage, err := qyt.LoadConfiguration(os.Args[1:])
	if err != nil {
		usage()
		os.Exit(1)
	}
	repo := panicOnResultErr(loadRepo(config))

	w := webview.New(false)
	defer w.Destroy()

	w.SetTitle("QYT App")
	w.SetSize(480, 320, webview.HintNone)

	registerSystemProxyBindings(w)
	panicOnErr(bindBackend(w, &backend{
		repo:   repo,
		config: config,
	}))
	setPageHTML(w)
	w.Run()
}

func setPageHTML(w webview.WebView) {
	w.SetHtml(string(panicOnResultErr(dir.ReadFile("embed/main.html"))))
}

func loadRepo(c qyt.Configuration) (*git.Repository, error) {
	return git.PlainOpenWithOptions(c.GitRepositoryPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
}

type backend struct {
	config models.Configuration
	repo   *git.Repository
}

func (b *backend) InitialConfiguration() (models.Configuration, error) {
	return b.config, nil
}

func (b *backend) ListBranchNames() ([]string, error) {
	refs, err := b.repo.Branches()
	if err != nil {
		return []string{}, err
	}
	defer refs.Close()
	var result []string
	for {
		var ref *plumbing.Reference
		ref, err = refs.Next()
		if err != nil {
			break
		}
		result = append(result, ref.Name().Short())
	}
	return result, nil
}
