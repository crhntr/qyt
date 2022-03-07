package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
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

	repo, err := loadRepo(qytConfig)
	if err != nil {
		log.Fatalf("failed to load repository: %s", err)
	}

	qa := initApp(qytConfig, mainWindow, repo)
	defer qa.Close()
	qa.branchEntry.SetText(qytConfig.BranchFilter)
	qa.pathEntry.SetText(qytConfig.FileNameFilter)
	qa.queryEntry.SetText(qytConfig.Query)
	go qa.Run()
	mainWindow.SetContent(qa.view)
	mainWindow.ShowAndRun()
}

type qytApp struct {
	config    qyt.Configuration
	repo      *git.Repository
	expParser yqlib.ExpressionParser

	window       fyne.Window
	view         *container.Split
	form         *widget.Form
	commitButton *widget.Button
	branchEntry,
	pathEntry,
	queryEntry *widget.Entry
	errMessage *widget.Label

	branchTabs *container.AppTabs

	branchC,
	pathC,
	queryC chan string

	copyRequestC, commitC chan struct{}
}

func initApp(config qyt.Configuration, mainWindow fyne.Window, repo *git.Repository) *qytApp {
	qa := &qytApp{
		repo:      repo,
		config:    config,
		expParser: yqlib.NewExpressionParser(),

		window:      mainWindow,
		form:        widget.NewForm(),
		branchEntry: widget.NewEntry(),
		pathEntry:   widget.NewEntry(),
		queryEntry:  widget.NewEntry(),
		branchTabs:  container.NewAppTabs(),
		errMessage:  widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	}
	qa.commitC = make(chan struct{})
	qa.commitButton = widget.NewButton("Commit", qa.triggerCommit)
	qa.view = container.NewVSplit(container.NewVBox(qa.form, qa.commitButton, qa.errMessage), qa.branchTabs)

	qa.branchEntry.Validator = func(s string) error {
		_, err := regexp.Compile(s)
		return err
	}
	qa.pathEntry.Validator = func(s string) error {
		_, err := regexp.Compile(s)
		return err
	}
	qa.queryEntry.Validator = func(s string) error {
		_, err := qa.expParser.ParseExpression(s)
		return err
	}

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

func loadRepo(c qyt.Configuration) (*git.Repository, error) {
	return git.PlainOpenWithOptions(c.GitRepositoryPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
}

func (qa qytApp) disableInput() {
	qa.branchEntry.Disable()
	qa.pathEntry.Disable()
	qa.queryEntry.Disable()
	qa.commitButton.Disable()
}

func (qa qytApp) enableInput() {
	qa.branchEntry.Enable()
	qa.pathEntry.Enable()
	qa.queryEntry.Enable()
	qa.commitButton.Enable()
}

func (qa qytApp) Close() {
	qa.disableInput()
	close(qa.branchC)
	close(qa.queryC)
	close(qa.pathC)
}

func (qa qytApp) Run() func() {
	branchFilter, fileFilter, exp, initialErr := qa.loadInitialData(qa.repo, qa.expParser)
	if initialErr != nil {
		qa.errMessage.SetText(initialErr.Error())
		qa.errMessage.Show()
	}
	qa.runQuery(qa.repo, branchFilter, fileFilter, exp)

	defer log.Println("done running")

	for {
		qa.enableInput()
		select {
		case <-qa.commitC:
			qa.disableInput()
			commitTemplate, branchPrefix, newBranches, submitted := qa.openCommitDialog(qa.config)
			if !submitted {
				break
			}
			qa.commit(commitTemplate, branchPrefix, !newBranches)
			var err error
			qa.repo, err = loadRepo(qa.config)
			if err != nil {
				log.Fatal(err)
			}
			qa.runQuery(qa.repo, branchFilter, fileFilter, exp)
		case <-qa.copyRequestC:
			qa.copyToClipboard()
		case b := <-qa.branchC:
			qa.disableInput()
			bf, err := regexp.Compile(b)
			if err != nil {
				qa.errMessage.SetText(err.Error())
				qa.errMessage.Show()
				break
			}
			branchFilter = bf
			qa.runQuery(qa.repo, branchFilter, fileFilter, exp)
		case p := <-qa.pathC:
			qa.disableInput()
			ff, err := regexp.Compile(p)
			if err != nil {
				qa.errMessage.SetText(err.Error())
				qa.errMessage.Show()
				break
			}
			fileFilter = ff
			qa.runQuery(qa.repo, branchFilter, fileFilter, exp)
		case q := <-qa.queryC:
			qa.disableInput()
			ex, err := qa.expParser.ParseExpression(q)
			if err != nil {
				qa.errMessage.SetText(err.Error())
				qa.errMessage.Show()
				break
			}
			exp = ex
			qa.runQuery(qa.repo, branchFilter, fileFilter, exp)
		}
	}
}

func (qa qytApp) openCommitDialog(con qyt.Configuration) (commitTemplate, branchPrefix string, newBranches bool, submitted bool) {
	commitTemplateEntree := widget.NewEntry()
	commitTemplateEntree.SetText(con.CommitTemplate)

	branchPrefixEntree := widget.NewEntry()
	branchPrefixEntree.SetText(con.NewBranchPrefix)

	newBranchesCheckbox := widget.NewCheck("", func(checked bool) {})

	formItems := []*widget.FormItem{
		widget.NewFormItem("Message", commitTemplateEntree),
		widget.NewFormItem("Branch Prefix", branchPrefixEntree),
		widget.NewFormItem("New Branches", newBranchesCheckbox),
	}

	c := make(chan struct{})
	dialog.ShowForm("Commit", "Commit", "Cancel", formItems, func(s bool) {
		defer close(c)
		commitTemplate = commitTemplateEntree.Text
		branchPrefix = branchPrefixEntree.Text
		newBranches = newBranchesCheckbox.Checked
		submitted = s
	}, qa.window)
	<-c
	log.Println(commitTemplate, branchPrefix, newBranches, submitted)
	return
}

const (
	FileViewNameResult = "Result"
	FileViewNameDiff   = "Diff"
)

func (qa qytApp) loadInitialData(repo *git.Repository, expParser yqlib.ExpressionParser) (*regexp.Regexp, *regexp.Regexp, *yqlib.ExpressionNode, error) {
	exp, err := expParser.ParseExpression(qa.queryEntry.Text)
	if err != nil {
		return nil, nil, nil, err
	}
	fileFilter, err := regexp.Compile(qa.pathEntry.Text)
	if err != nil {
		return nil, nil, nil, err
	}
	branchFilter, err := regexp.Compile(qa.branchEntry.Text)
	if err != nil {
		return nil, nil, nil, err
	}
	qa.runQuery(repo, branchFilter, fileFilter, exp)
	return branchFilter, fileFilter, exp, nil
}

func (qa qytApp) runQuery(repo *git.Repository, branchFilter, fileFilter *regexp.Regexp, queryExp *yqlib.ExpressionNode) {
	qa.errMessage.Hide()
	qa.errMessage.SetText("")
	qa.branchTabs.SetItems(nil)

	references, err := qyt.MatchingBranches(branchFilter.String(), qa.repo, false)
	if err != nil {
		qa.errMessage.SetText(err.Error())
		qa.errMessage.Show()
		return
	}

	buf := new(bytes.Buffer)
	for _, ref := range references {
		fileTabs := container.NewAppTabs()
		bt := container.NewTabItem(ref.Name().Short(), fileTabs)
		fileTabs.OnSelected = func(item *container.TabItem) {
			qa.selectAllFilesWithPath(item.Text)
		}
		fileTabs.SetTabLocation(container.TabLocationLeading)
		qa.branchTabs.Append(bt)

		var obj object.Object
		obj, err := repo.Object(plumbing.CommitObject, ref.Hash())
		if err != nil {
			qa.errMessage.SetText(err.Error())
			qa.errMessage.Show()
			return
		}
		count := 0
		err = qyt.HandleMatchingFiles(obj, fileFilter, func(file *object.File) (err error) {
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
				container.NewTabItem(FileViewNameResult, box),
				container.NewTabItem(FileViewNameDiff, rt),
			)
			fileViews.OnSelected = func(item *container.TabItem) {
				qa.selectAllFileViewsWithName(item.Text)
			}
			fileViews.SetTabLocation(container.TabLocationBottom)
			fileTabs.Append(container.NewTabItem(file.Name, fileViews))
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

func (qa qytApp) triggerCommit() {
	qa.commitC <- struct{}{}
}

func (qa qytApp) copyToClipboard() {
	qa.window.Clipboard().SetContent(qa.selectedFileContents())
}

func (qa qytApp) selectedFileContents() string {
	fileViews, _, ok := qa.selectedFileViews()
	if !ok {
		panic("failed to get selected files")
	}
	if len(fileViews.Items) == 0 {
		return ""
	}
	cont, ok := fileViews.Items[0].Content.(*fyne.Container)
	if !ok || len(cont.Objects) <= 1 {
		panic("failed to get result view")
	}
	rt, ok := cont.Objects[1].(*widget.RichText)
	if !ok {
		panic("failed to get selected files")
	}
	return rt.String()
}

func (qa qytApp) selectedFileViews() (*container.AppTabs, string, bool) {
	branchTab := qa.branchTabs.Selected()
	fileTabs, ok := branchTab.Content.(*container.AppTabs)
	if !ok {
		panic("failed to get selected branch")
	}
	ft := fileTabs.Selected()
	tabs, ok := ft.Content.(*container.AppTabs)
	return tabs, ft.Text, ok
}

func (qa qytApp) selectAllFilesWithPath(s string) {
	for _, branchTab := range qa.branchTabs.Items {
		fileTabs := branchTab.Content.(*container.AppTabs)
		for i, fileTab := range fileTabs.Items {
			if fileTab.Text == s {
				fileTabs.SelectIndex(i)
				break
			}
		}
	}
}

func (qa qytApp) selectAllFileViewsWithName(s string) {
	for _, branchTab := range qa.branchTabs.Items {
		fileTabs := branchTab.Content.(*container.AppTabs)
		for _, fileTab := range fileTabs.Items {
			fileViews := fileTab.Content.(*container.AppTabs)
			for i, fileView := range fileViews.Items {
				if fileView.Text == s {
					fileViews.SelectIndex(i)
					break
				}
			}
		}
	}
}

func (qa qytApp) commit(commitTemplate, branchPrefix string, existingBranches bool) {
	sig, err := getSignature(qa.repo, time.Now())
	if err != nil {
		qa.errMessage.SetText(err.Error())
		qa.errMessage.Show()
		return
	}
	if existingBranches {
		branchPrefix = ""
	}
	err = qyt.Apply(qa.repo,
		qa.queryEntry.Text,
		qa.branchEntry.Text,
		qa.pathEntry.Text,
		commitTemplate,
		branchPrefix,
		sig, false, existingBranches,
	)
	if err != nil {
		qa.errMessage.SetText(err.Error())
		qa.errMessage.Show()
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
