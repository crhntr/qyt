package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"regexp"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"gopkg.in/op/go-logging.v1"

	"github.com/crhntr/qyt"
)

func init() {
	backend := logging.NewLogBackend(io.Discard, "", 0)
	logging.SetLevel(logging.CRITICAL, "github.com/mikefarah/yq/v4")
	logging.SetBackend(backend)
}

func main() {
	qytConfig, usage, err := qyt.LoadConfiguration(os.Args[1:])
	if err != nil {
		usage()
		os.Exit(1)
	}

	repo, err := loadRepo(qytConfig)
	if err != nil {
		log.Fatalf("failed to load repository: %s", err)
	}

	qa := initApp(app.New, qytConfig, repo)
	defer qa.Close()
	qa.runQuery(qa.repo)
	qa.window.ShowAndRun()
}

type qytApp struct {
	sync.Mutex
	config    qyt.Configuration
	repo      *git.Repository
	expParser yqlib.ExpressionParserInterface

	window fyne.Window
	view   *container.Split
	form   *widget.Form
	newBranchesCheckbox,
	commitResultCheckbox *widget.Check

	commitTemplateEntry,
	branchPrefixEntry,
	branchEntry,
	pathEntry,
	queryEntry *widget.Entry
	errMessage *widget.Label

	branchTabs *container.AppTabs

	loadRepo func(configuration qyt.Configuration) (*git.Repository, error)
}

const (
	formLabelYAMLQuery    = "YAML Query"
	formLabelBranchRegExp = "Branch RegExp"
	formLabelFileRegExp   = "File RegExp"
	formLabelCommitResult = "Commit Result"
	formLabelMessage      = "Message"
	formLabelNewBranches  = "New Branches"
	formLabelBranchPrefix = "Branch Prefix"
	formSubmitButtonText  = "Run Query"
)

func initApp(createApp func() fyne.App, config qyt.Configuration, repo *git.Repository) *qytApp {
	myApp := createApp()
	myApp.Settings().SetTheme(qytTheme{
		Theme: theme.DefaultTheme(),
	})
	mainWindow := myApp.NewWindow("qyt = yq * git")
	mainWindow.Resize(fyne.NewSize(1200, 900))
	mainWindow.SetFixedSize(false)

	qa := &qytApp{
		repo:                repo,
		config:              config,
		expParser:           yqlib.ExpressionParser,
		window:              mainWindow,
		form:                widget.NewForm(),
		branchEntry:         widget.NewEntry(),
		pathEntry:           widget.NewEntry(),
		queryEntry:          widget.NewEntry(),
		commitTemplateEntry: widget.NewEntry(),
		branchPrefixEntry:   widget.NewEntry(),
		branchTabs:          container.NewAppTabs(),
		errMessage:          widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
	}

	qa.branchPrefixEntry.Disable()
	qa.newBranchesCheckbox = widget.NewCheck("", func(checked bool) {
		qa.Lock()
		defer qa.Unlock()

		if checked {
			qa.branchPrefixEntry.Enable()
		} else {
			qa.branchPrefixEntry.Disable()
		}
		qa.form.Refresh()
	})

	qa.commitTemplateEntry.Disable()
	qa.branchPrefixEntry.Disable()
	qa.newBranchesCheckbox.Disable()
	qa.commitResultCheckbox = widget.NewCheck("Commit Result", func(checked bool) {
		qa.Lock()
		defer qa.Unlock()

		if checked {
			qa.commitTemplateEntry.Enable()
			qa.newBranchesCheckbox.Enable()
		} else {
			qa.commitTemplateEntry.Disable()
			qa.branchPrefixEntry.Disable()
			qa.newBranchesCheckbox.Disable()
		}
		qa.form.Refresh()
	})

	qa.commitTemplateEntry.SetText(qa.config.CommitTemplate)
	qa.commitTemplateEntry.MultiLine = true
	qa.branchPrefixEntry.SetText(qa.config.NewBranchPrefix)

	qa.view = container.NewVSplit(container.NewVBox(qa.form, qa.errMessage), qa.branchTabs)

	qa.branchEntry.Validator = func(s string) error {
		_, err := regexp.Compile(s)
		return err
	}
	qa.pathEntry.Validator = func(s string) error {
		_, err := regexp.Compile(s)
		return err
	}
	qa.queryEntry.Validator = func(s string) error {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("recovered from query validation: %s", r)
			}
		}()
		if s == "" {
			return errors.New("empty query")
		}
		_, err := yqlib.ExpressionParser.ParseExpression(s)
		return err
	}
	qa.queryEntry.MultiLine = true
	qa.branchEntry.SetText(qa.config.BranchFilter)
	qa.pathEntry.SetText(qa.config.FileNameFilter)
	qa.queryEntry.SetText(qa.config.Query)

	qa.form.SubmitText = formSubmitButtonText
	qa.form.OnSubmit = func() {
		qa.disableInput()
		qa.runQuery(qa.repo)
		qa.enableInput()
	}
	qa.form.Append(formLabelYAMLQuery, qa.queryEntry)
	qa.form.Append(formLabelBranchRegExp, qa.branchEntry)
	qa.form.Append(formLabelFileRegExp, qa.pathEntry)
	qa.form.Append(formLabelCommitResult, qa.commitResultCheckbox)
	qa.form.Append(formLabelMessage, qa.commitTemplateEntry)
	qa.form.Append(formLabelNewBranches, qa.newBranchesCheckbox)
	qa.form.Append(formLabelBranchPrefix, qa.branchPrefixEntry)

	qa.window.SetContent(qa.view)

	return qa
}

func loadRepo(c qyt.Configuration) (*git.Repository, error) {
	return git.PlainOpenWithOptions(c.GitRepositoryPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
}

func (qa *qytApp) disableInput() {
	qa.Lock()
	defer qa.Unlock()

	qa.form.Disable()

	qa.branchEntry.Disable()
	qa.pathEntry.Disable()
	qa.queryEntry.Disable()
	qa.commitTemplateEntry.Disable()
	qa.branchPrefixEntry.Disable()

	qa.newBranchesCheckbox.Disable()
	qa.commitResultCheckbox.Disable()
}

func (qa *qytApp) enableInput() {
	qa.Lock()
	defer qa.Unlock()

	qa.branchEntry.Enable()
	qa.pathEntry.Enable()
	qa.queryEntry.Enable()

	if qa.commitResultCheckbox.Checked {
		qa.commitTemplateEntry.Enable()
		qa.branchPrefixEntry.Enable()

		qa.newBranchesCheckbox.Enable()
	}
	qa.commitResultCheckbox.Enable()

	qa.form.Enable()
}

func (qa *qytApp) Close() {
	qa.disableInput()
}

const (
	FileViewNameResult = "Result"
	FileViewNameDiff   = "Diff"
)

func (qa *qytApp) parseFields(b, f, q string) (*regexp.Regexp, *regexp.Regexp, *yqlib.ExpressionNode, error) {
	qa.Lock()
	defer qa.Unlock()
	exp, err := qa.expParser.ParseExpression(q)
	if err != nil {
		return nil, nil, nil, err
	}
	fileFilter, err := regexp.Compile(f)
	if err != nil {
		return nil, nil, nil, err
	}
	branchFilter, err := regexp.Compile(b)
	if err != nil {
		return nil, nil, nil, err
	}
	return branchFilter, fileFilter, exp, nil
}

func (qa *qytApp) runQuery(repo *git.Repository) {
	b := qa.branchEntry.Text
	f := qa.pathEntry.Text
	q := qa.queryEntry.Text
	branchFilter, fileFilter, queryExp, err := qa.parseFields(b, f, q)
	if err != nil {
		qa.displayError(err)
		return
	}

	if qa.commitResultCheckbox.Checked {
		commitTemplate := qa.commitTemplateEntry.Text
		branchPrefix := qa.branchPrefixEntry.Text
		newBranches := qa.newBranchesCheckbox.Checked
		qa.commit(commitTemplate, branchPrefix, !newBranches)
		var err error
		qa.repo, err = loadRepo(qa.config)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("$ qyt apply -b %q -f %q -q %q -p %s -m %q\n", branchFilter, fileFilter, q, branchPrefix, commitTemplate)
	} else {
		fmt.Printf("$ qyt query -b %q -f %q -q %q\n", branchFilter, fileFilter, q)
	}

	qa.clearBranchesAndError()

	references, err := qyt.MatchingBranches(branchFilter.String(), qa.repo, false)
	if err != nil {
		qa.displayError(err)
		return
	}

	buf := new(bytes.Buffer)
	count := 0
	for _, ref := range references {
		fileTabs := qa.createNewBranchTab(ref)

		var obj object.Object
		obj, err := repo.Object(plumbing.CommitObject, ref.Hash())
		if err != nil {
			qa.displayError(err)
			return
		}
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

			qa.createFilesView(fileTabs, file.Name, buf.String())
			return nil
		})
		if err != nil {
			qa.displayError(err)
			continue
		}
	}
	if count == 0 && err == nil {
		qa.displayError(fmt.Errorf("no matching files"))
		return
	}
}

func (qa *qytApp) createNewBranchTab(ref plumbing.Reference) *container.AppTabs {
	qa.Lock()
	defer qa.Unlock()

	fileTabs := container.NewAppTabs()
	bt := container.NewTabItem(ref.Name().Short(), fileTabs)
	fileTabs.OnSelected = func(item *container.TabItem) {
		qa.selectAllFilesWithPath(item.Text)
	}
	fileTabs.SetTabLocation(container.TabLocationLeading)
	qa.branchTabs.Append(bt)
	return fileTabs
}

func (qa *qytApp) clearBranchesAndError() {
	qa.Lock()
	defer qa.Unlock()

	qa.errMessage.Hide()
	qa.errMessage.SetText("")
	qa.branchTabs.SetItems(nil)
}

func (qa *qytApp) displayError(err error) {
	qa.Lock()
	defer qa.Unlock()

	qa.errMessage.SetText(err.Error())
	qa.errMessage.Show()
}

func (qa *qytApp) createFilesView(fileTabs *container.AppTabs, fileName, fileContents string) {
	qa.Lock()
	defer qa.Unlock()

	toolbar := widget.NewToolbar()
	toolbar.Append(widget.NewToolbarAction(theme.ContentCopyIcon(), func() {
		qa.window.Clipboard().SetContent(fileContents)
	}))
	contents := widget.NewRichTextWithText(fileContents)
	contents.Wrapping = fyne.TextWrapOff
	box := container.NewVBox(toolbar, contents)
	box.Layout.Layout(box.Objects, fyne.NewSize(300, 400))

	fileViews := container.NewAppTabs(
		container.NewTabItem(FileViewNameResult, container.NewScroll(box)),
	)
	fileViews.OnSelected = func(item *container.TabItem) {
		qa.selectAllFileViewsWithName(item.Text)
	}
	fileViews.SetTabLocation(container.TabLocationBottom)

	fileTabs.Append(container.NewTabItem(fileName, fileViews))
}

func (qa *qytApp) selectedFileContents() string {
	fileViews, _, ok := qa.selectedFileViews()
	if !ok {
		panic("failed to get selected files")
	}
	if len(fileViews.Items) == 0 {
		return ""
	}
	cont, ok := fileViews.Items[0].Content.(*container.Scroll).Content.(*fyne.Container)
	if !ok || len(cont.Objects) <= 1 {
		panic("failed to get result view")
	}
	rt, ok := cont.Objects[1].(*widget.RichText)
	if !ok {
		panic("failed to get selected files")
	}
	return rt.String()
}

func (qa *qytApp) selectedFileViews() (*container.AppTabs, string, bool) {
	branchTab := qa.branchTabs.Selected()
	fileTabs, ok := branchTab.Content.(*container.AppTabs)
	if !ok {
		panic("failed to get selected branch")
	}
	ft := fileTabs.Selected()
	tabs, ok := ft.Content.(*container.AppTabs)
	return tabs, ft.Text, ok
}

func (qa *qytApp) selectAllFilesWithPath(s string) {
	if !qa.TryLock() {
		return
	}
	defer qa.Unlock()

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

func (qa *qytApp) selectAllFileViewsWithName(s string) {
	if !qa.TryLock() {
		return
	}
	defer qa.Unlock()

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

func (qa *qytApp) commit(commitTemplate, branchPrefix string, existingBranches bool) {
	sig, err := getSignature(qa.repo, time.Now())
	if err != nil {
		qa.displayError(err)
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
		qa.displayError(err)
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

type qytTheme struct {
	fyne.Theme
}

const InsertColor = "Insert"

func (qt qytTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == InsertColor {
		return color.NRGBA{G: 255, A: 255}
	}
	return qt.Theme.Color(name, variant)
}
