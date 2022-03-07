package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"github.com/sergi/go-diff/diffmatchpatch"
	"gopkg.in/op/go-logging.v1"

	"github.com/crhntr/qyt"
)

func main() {
	backend := logging.NewLogBackend(io.Discard, "", 0)
	logging.SetBackend(backend)

	qytConfig, usage, err := qyt.LoadConfiguration()
	if err != nil {
		usage()
		os.Exit(1)
	}

	myApp := app.New()
	mainWindow := myApp.NewWindow("qyt = yq * git")
	mainWindow.Resize(fyne.NewSize(800, 600))

	repo, err := git.PlainOpenWithOptions(qytConfig.GitRepositoryPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", err)
		os.Exit(1)
	}

	qa := initApp(mainWindow)
	defer qa.Close()
	qa.branchEntry.SetText(qytConfig.BranchFilter)
	qa.pathEntry.SetText(qytConfig.FileNameFilter)
	qa.queryEntry.SetText(qytConfig.Query)
	go qa.Run(repo)
	mainWindow.SetContent(qa.view)
	mainWindow.ShowAndRun()
}

type qytApp struct {
	window fyne.Window
	view   *container.Split
	form   *widget.Form
	branchEntry,
	pathEntry,
	queryEntry *widget.Entry
	errMessage *widget.Label

	branchTabs *container.AppTabs

	branchC,
	pathC,
	queryC chan string

	copyRequestC chan struct{}
}

func initApp(mainWindow fyne.Window) *qytApp {
	qa := &qytApp{
		window:      mainWindow,
		form:        widget.NewForm(),
		branchEntry: widget.NewEntry(),
		pathEntry:   widget.NewEntry(),
		queryEntry:  widget.NewEntry(),
		branchTabs:  container.NewAppTabs(),
		errMessage:  widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	}
	qa.view = container.NewVSplit(container.NewVBox(qa.form, qa.errMessage), qa.branchTabs)

	qa.form.Append("YAML Query", qa.queryEntry)
	qa.form.Append("Branch RegExp", qa.branchEntry)
	qa.form.Append("File RegExp", qa.pathEntry)
	qa.branchC = make(chan string)
	qa.queryC = make(chan string)
	qa.pathC = make(chan string)
	qa.copyRequestC = make(chan struct{})
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
	expParser := yqlib.NewExpressionParser()

	refs, fileFilter, exp, initialErr := qa.loadInitialData(repo, expParser)
	if initialErr != nil {
		qa.errMessage.SetText(initialErr.Error())
		qa.errMessage.Show()
	}
	qa.runQuery(repo, refs, fileFilter, exp)

	for {
		select {
		case <-qa.copyRequestC:
			qa.copyToClipboard()
		case b := <-qa.branchC:
			rs, err := qyt.MatchingBranches(b, repo, false)
			if err != nil {
				qa.errMessage.SetText(err.Error())
				qa.errMessage.Show()
				break
			}
			refs = rs
			qa.runQuery(repo, refs, fileFilter, exp)
		case p := <-qa.pathC:
			ff, err := regexp.Compile(p)
			if err != nil {
				qa.errMessage.SetText(err.Error())
				qa.errMessage.Show()
				break
			}
			fileFilter = ff
			qa.runQuery(repo, refs, fileFilter, exp)
		case q := <-qa.queryC:
			ex, err := expParser.ParseExpression(q)
			if err != nil {
				qa.errMessage.SetText(err.Error())
				qa.errMessage.Show()
				break
			}
			exp = ex
			qa.runQuery(repo, refs, fileFilter, exp)
		}
	}
}

func (qa qytApp) loadInitialData(repo *git.Repository, expParser yqlib.ExpressionParser) ([]plumbing.Reference, *regexp.Regexp, *yqlib.ExpressionNode, error) {
	exp, err := expParser.ParseExpression(qa.queryEntry.Text)
	if err != nil {
		return nil, nil, nil, err
	}
	fileFilter, err := regexp.Compile(qa.pathEntry.Text)
	if err != nil {
		return nil, nil, nil, err
	}
	refs, err := qyt.MatchingBranches(qa.branchEntry.Text, repo, false)
	if err != nil {
		return nil, fileFilter, exp, err
	}
	qa.runQuery(repo, refs, fileFilter, exp)
	return refs, fileFilter, exp, nil
}

func (qa qytApp) runQuery(repo *git.Repository, references []plumbing.Reference, fileNameMatcher *regexp.Regexp, queryExp *yqlib.ExpressionNode) {
	qa.errMessage.Hide()
	qa.errMessage.SetText("")
	qa.branchTabs.SetItems(nil)

	buf := new(bytes.Buffer)
	for _, ref := range references {
		resultView := container.NewAppTabs()
		bt := container.NewTabItem(ref.Name().Short(), resultView)
		resultView.SetTabLocation(container.TabLocationLeading)
		qa.branchTabs.Append(bt)

		var obj object.Object
		obj, err := repo.Object(plumbing.CommitObject, ref.Hash())
		if err != nil {
			qa.errMessage.SetText(err.Error())
			qa.errMessage.Show()
			return
		}
		count := 0
		err = qyt.HandleMatchingFiles(obj, fileNameMatcher, func(file *object.File) (err error) {
			count++
			rc, err := file.Reader()
			if err != nil {
				return err
			}
			defer func() {
				err = rc.Close()
			}()
			input, err := io.ReadAll(rc)
			if err != nil {
				return err
			}
			buf.Reset()
			err = qyt.ApplyExpression(buf, bytes.NewReader(input), queryExp, file.Name, qyt.NewScope(ref, file), false)
			if err != nil {
				return err
			}

			dmp := diffmatchpatch.New()
			diffs := dmp.DiffMain(string(input), buf.String(), true)

			diffSegments := make([]widget.RichTextSegment, 0, len(diffs))
			for _, d := range diffs {
				style := widget.RichTextStyle{
					Inline:    false,
					SizeName:  theme.SizeNameText,
					TextStyle: fyne.TextStyle{Monospace: true},
				}
				switch d.Type {
				case diffmatchpatch.DiffDelete:
					style.ColorName = theme.ColorNameError
				case diffmatchpatch.DiffInsert:
					style.ColorName = theme.ColorNamePrimary
				case diffmatchpatch.DiffEqual:
					style.ColorName = theme.ColorNameForeground
				}
				style.Inline = !strings.HasSuffix(d.Text, "\n")
				diffSegments = append(diffSegments, &widget.TextSegment{
					Text:  strings.TrimSuffix(d.Text, "\n"),
					Style: style,
				})
			}
			rt := widget.NewRichText(diffSegments...)

			toolbar := widget.NewToolbar()
			toolbar.Append(widget.NewToolbarAction(theme.ContentCopyIcon(), qa.triggerCopyToClipboard))
			contents := widget.NewRichTextWithText(buf.String())
			contents.Wrapping = fyne.TextWrapOff
			box := container.NewVBox(toolbar, contents)
			box.Layout.Layout(box.Objects, fyne.NewSize(300, 400))

			fileViews := container.NewAppTabs(
				container.NewTabItem("Result", box),
				container.NewTabItem("Diff", rt),
			)
			fileViews.SetTabLocation(container.TabLocationBottom)
			resultView.Append(container.NewTabItem(file.Name, fileViews))
			return nil
		})
		if count == 0 && err != nil {
			err = fmt.Errorf("no matching files for ref %s", ref.Name())
		}
		if err != nil {
			qa.errMessage.SetText(err.Error())
			qa.errMessage.Show()
		}
	}
}

func (qa qytApp) triggerCopyToClipboard() {
	qa.copyRequestC <- struct{}{}
}

func (qa qytApp) copyToClipboard() {
	branchTab := qa.branchTabs.Selected()
	appTabs, ok := branchTab.Content.(*container.AppTabs)
	if !ok {
		return
	}
	fileWidgetContainer, ok := appTabs.Selected().Content.(*fyne.Container)
	if !ok || len(fileWidgetContainer.Objects) <= 1 {
		return
	}
	rt, ok := fileWidgetContainer.Objects[1].(*widget.RichText)
	if !ok {
		return
	}
	qa.window.Clipboard().SetContent(rt.String())
}
