package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"path"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"gopkg.in/op/go-logging.v1"

	"github.com/crhntr/qyt"
)

const (
	defaultFieldBranchRegex  = ".*"
	defaultFieldYQExpression = "."
	defaultFieldFileFilter   = "*"
)

func main() {
	backend := logging.NewLogBackend(io.Discard, "", 0)
	logging.SetBackend(backend)

	myApp := app.New()
	mainWindow := myApp.NewWindow("qyt = yq * git")
	mainWindow.Resize(fyne.NewSize(800, 600))

	repo, err := git.PlainOpen("config")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", err)
		os.Exit(1)
	}

	var (
		branchC = make(chan string)
		pathC   = make(chan string)
		queryC  = make(chan string)
	)

	form := widget.NewForm()
	branchEntree := widget.NewEntry()
	form.Append("Branch Query", branchEntree)
	qyQueryEntree := widget.NewEntry()
	form.Append("YQ Expression", qyQueryEntree)
	fileBlobEntree := widget.NewEntry()
	form.Append("File Glob", fileBlobEntree)

	handle := func(c chan string) func(in string) {
		return func(in string) {
			branchEntree.Disable()
			defer branchEntree.Enable()
			qyQueryEntree.Disable()
			defer qyQueryEntree.Enable()
			fileBlobEntree.Disable()
			defer fileBlobEntree.Enable()
			c <- in
		}
	}

	branchEntree.SetText(defaultFieldBranchRegex)
	qyQueryEntree.SetText(defaultFieldYQExpression)
	fileBlobEntree.SetText(defaultFieldFileFilter)

	branchTabs := container.NewAppTabs()
	branchEntree.OnSubmitted = handle(branchC)
	qyQueryEntree.OnSubmitted = handle(queryC)
	fileBlobEntree.OnSubmitted = handle(pathC)
	defer func() {
		branchEntree.Disable()
		qyQueryEntree.Disable()
		fileBlobEntree.Disable()
		close(branchC)
		close(queryC)
		close(pathC)
	}()

	go func() {
		var (
			expParser     = yqlib.NewExpressionParser()
			filePath      = defaultFieldFileFilter
			queryExp, _   = expParser.ParseExpression(defaultFieldYQExpression)
			references, _ = qyt.MatchingBranches(defaultFieldBranchRegex, repo, false)
			out           = new(bytes.Buffer)
		)

		updateUI(branchTabs, references, err, repo, filePath, queryExp)

	eventLoop:
		for {
			err = nil
			out.Reset()
			select {
			case b := <-branchC:
				references, err = qyt.MatchingBranches(b, repo, false)
				if err != nil {
					_, _ = fmt.Fprintln(os.Stderr, "failed to get matching branches", err)
					continue eventLoop
				}
			case p := <-pathC:
				_, err := path.Match(p, "")
				if err != nil {
					continue eventLoop
				}
				filePath = p
			case q := <-queryC:
				ex, err := expParser.ParseExpression(q)
				if err != nil {
					_, _ = fmt.Fprintln(os.Stderr, "failed to parse expression", err)
					continue eventLoop
				}
				queryExp = ex
			}

			updateUI(branchTabs, references, err, repo, filePath, queryExp)
		}
	}()

	mainView := container.NewVSplit(
		form,
		branchTabs,
	)

	mainWindow.SetContent(mainView)
	mainWindow.ShowAndRun()
}

func updateUI(branchTabs *container.AppTabs, references []plumbing.Reference, err error, repo *git.Repository, filePath string, queryExp *yqlib.ExpressionNode) {
	branchTabs.SetItems(nil)
	buf := new(bytes.Buffer)
	for _, ref := range references {
		resultView := container.NewAppTabs()
		bt := container.NewTabItem(ref.Name().Short(), resultView)
		resultView.SetTabLocation(container.TabLocationLeading)
		branchTabs.Append(bt)

		var obj object.Object
		obj, err = repo.Object(plumbing.CommitObject, ref.Hash())
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "failed to get commit", err)
			continue
		}
		count := 0
		err = qyt.HandleMatchingFiles(obj, filePath, func(file *object.File) error {
			count++
			rc, _ := file.Reader()
			defer func() {
				_ = rc.Close()
			}()
			buf.Reset()
			err := qyt.ApplyExpression(buf, rc, queryExp, file.Name, qyt.NewScope(ref, file), false)
			if err != nil {
				return err
			}
			resultView.Append(container.NewTabItem(file.Name, widget.NewLabel(buf.String())))
			return nil
		})
		if count == 0 {
			log.Printf("no files found for %s", ref.Name())
		}
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "failed run query", err)
			continue
		}
	}
}
