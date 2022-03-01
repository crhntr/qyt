package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"regexp"

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
	defaultFieldFileFilter   = `(.+)\.ya?ml`
)

func main() {
	backend := logging.NewLogBackend(io.Discard, "", 0)
	logging.SetBackend(backend)

	myApp := app.New()
	mainWindow := myApp.NewWindow("qyt = yq * git")
	mainWindow.Resize(fyne.NewSize(800, 600))

	repoPath := "."
	if len(os.Args) > 1 {
		repoPath = os.Args[1]
	}
	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", err)
		os.Exit(1)
	}

	qa := initQYTApp()
	defer qa.Close()
	go qa.Run(repo)
	mainWindow.SetContent(qa.view)
	mainWindow.ShowAndRun()
}

type qytApp struct {
	view *container.Split
	form *widget.Form
	branchEntry,
	pathEntry,
	queryEntry *widget.Entry
	errMessage *widget.Label

	branchTabs *container.AppTabs

	branchC,
	pathC,
	queryC chan string
}

func initQYTApp() *qytApp {
	qa := &qytApp{
		form:        widget.NewForm(),
		branchEntry: widget.NewEntry(),
		pathEntry:   widget.NewEntry(),
		queryEntry:  widget.NewEntry(),
		branchTabs:  container.NewAppTabs(),
		errMessage:  widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	}
	qa.view = container.NewVSplit(container.NewVBox(qa.form, qa.errMessage), qa.branchTabs)

	qa.branchEntry.SetText(defaultFieldBranchRegex)
	qa.pathEntry.SetText(defaultFieldFileFilter)
	qa.queryEntry.SetText(defaultFieldYQExpression)
	qa.form.Append("Branch Query", qa.branchEntry)
	qa.form.Append("YQ Expression", qa.queryEntry)
	qa.form.Append("File Glob", qa.pathEntry)
	qa.branchC = make(chan string)
	qa.queryC = make(chan string)
	qa.pathC = make(chan string)
	handle := func(c chan string) func(string) {
		return func(s string) { c <- s }
	}
	qa.branchEntry.OnSubmitted = handle(qa.branchC)
	qa.pathEntry.OnSubmitted = handle(qa.pathC)
	qa.queryEntry.OnSubmitted = handle(qa.queryC)
	return qa
}

func (qa qytApp) Close() {
	qa.branchEntry.Disable()
	qa.pathEntry.Disable()
	qa.queryEntry.Disable()
	close(qa.branchC)
	close(qa.queryC)
	close(qa.pathC)
}

func (qa qytApp) Run(repo *git.Repository) func() {
	var (
		expParser = yqlib.NewExpressionParser()
		out       = new(bytes.Buffer)
	)

	refs, exp, fileFilter, initialErr := qa.loadInitialData(repo, expParser)
	if initialErr != nil {
		qa.errMessage.SetText(initialErr.Error())
		qa.errMessage.Show()
	}

eventLoop:
	for {
		out.Reset()

		select {
		case b := <-qa.branchC:
			rs, err := qyt.MatchingBranches(b, repo, false)
			if err != nil {
				qa.errMessage.SetText(err.Error())
				qa.errMessage.Show()
				continue eventLoop
			}
			refs = rs
		case p := <-qa.pathC:
			ff, err := regexp.Compile(p)
			if err != nil {
				qa.errMessage.SetText(err.Error())
				qa.errMessage.Show()
				continue eventLoop
			}
			fileFilter = ff
		case q := <-qa.queryC:
			ex, err := expParser.ParseExpression(q)
			if err != nil {
				qa.errMessage.SetText(err.Error())
				qa.errMessage.Show()
				continue eventLoop
			}
			exp = ex
		}
		qa.errMessage.Hide()
		qa.errMessage.SetText("")
		err := runQuery(qa.branchTabs, refs, repo, fileFilter, exp)
		if err != nil {
			qa.errMessage.SetText(err.Error())
			qa.errMessage.Show()
			continue eventLoop
		}
	}
}

func (qa qytApp) loadInitialData(repo *git.Repository, expParser yqlib.ExpressionParser) ([]plumbing.Reference, *yqlib.ExpressionNode, *regexp.Regexp, error) {
	exp, err := expParser.ParseExpression(defaultFieldYQExpression)
	if err != nil {
		return nil, nil, nil, err
	}
	fileFilter, err := regexp.Compile(defaultFieldFileFilter)
	if err != nil {
		return nil, nil, nil, err
	}
	refs, err := qyt.MatchingBranches(defaultFieldBranchRegex, repo, false)
	if err != nil {
		return nil, exp, fileFilter, err
	}
	err = runQuery(qa.branchTabs, refs, repo, fileFilter, exp)
	if err != nil {
		return refs, exp, fileFilter, err
	}
	return refs, exp, fileFilter, nil
}

func runQuery(branchTabs *container.AppTabs, references []plumbing.Reference, repo *git.Repository, fileNameMatcher *regexp.Regexp, queryExp *yqlib.ExpressionNode) error {
	branchTabs.SetItems(nil)
	buf := new(bytes.Buffer)
	for _, ref := range references {
		resultView := container.NewAppTabs()
		bt := container.NewTabItem(ref.Name().Short(), resultView)
		resultView.SetTabLocation(container.TabLocationLeading)
		branchTabs.Append(bt)

		var obj object.Object
		obj, err := repo.Object(plumbing.CommitObject, ref.Hash())
		if err != nil {
			return err
		}
		count := 0
		err = handleMatchingFiles(obj, fileNameMatcher, func(file *object.File) error {
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
			return fmt.Errorf("no matching files for ref %s", ref.Name())
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func handleMatchingFiles(obj object.Object, re *regexp.Regexp, fn func(file *object.File) error) error {
	switch o := obj.(type) {
	case *object.Commit:
		t, err := o.Tree()
		if err != nil {
			return err
		}
		return handleMatchingFiles(t, re, fn)
	case *object.Tag:
		target, err := o.Object()
		if err != nil {
			return err
		}
		return handleMatchingFiles(target, re, fn)
	case *object.Tree:
		return o.Files().ForEach(func(file *object.File) error {
			if re != nil {
				if !re.MatchString(file.Name) {
					return nil
				}
			}
			return fn(file)
		})
	//case *object.Blob:
	default:
		return object.ErrUnsupportedObject
	}
}
