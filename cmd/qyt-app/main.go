package main

import (
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"

	"fyne.io/fyne/v2/app"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"gopkg.in/op/go-logging.v1"

	"github.com/crhntr/qyt"
)

func main() {
	backend := logging.NewLogBackend(io.Discard, "", 0)
	logging.SetBackend(backend)

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

	c := newController(repo)

	myApp := app.New()
	c.view = createView(myApp, c)

	c.view.SetBranchInputValue(defaultFieldBranchRegex())
	c.view.Window().ShowAndRun()
}

type qytController struct {
	repo *git.Repository

	branchExp, fileExp *regexp.Regexp
	queryExpression    *yqlib.ExpressionNode

	selectedBranchIndex, selectedFileIndex int

	branches    []plumbing.Reference
	files       []object.File
	filesCache  map[plumbing.Reference][]object.File
	queryResult string

	view view
}

func newController(repo *git.Repository) *qytController {
	return &qytController{
		repo:       repo,
		filesCache: make(map[plumbing.Reference][]object.File),
	}
}

func (qa *qytController) SetSelectedBranch(i int) {
	qa.selectedBranchIndex = i
	log.Println("SetSelectedBranch", i)
	if len(qa.branches) == 0 {
		return
	}
	ref := qa.branches[i]
	qa.files = qa.filesCache[ref]
	qa.view.SetFiles(ref, qa.files, 0)
}

func (qa *qytController) SetSelectedFile(ref plumbing.Reference, i int) {
	qa.selectedFileIndex = i
	log.Println("SetSelectedFile", ref.Hash(), i)
}

func (qa *qytController) SetInputBranchFilter(s string) {
	log.Println("SetInputBranchFilter", s)

	qa.view.ClearErrorMessage()

	refs, err := qyt.MatchingBranches(s, qa.repo, false)
	qa.branches = refs
	if err != nil {
		qa.view.SetBranches(nil, 0)
		qa.view.SetErrorMessage(err.Error())
		return
	}
	if len(refs) == 0 {
		qa.view.SetBranches(nil, 0)
		return
	}

	for k := range qa.filesCache {
		delete(qa.filesCache, k)
	}

	for _, ref := range refs {
		c, err := qa.repo.CommitObject(ref.Hash())
		if err != nil {
			continue
		}
		fileIter, err := c.Files()
		if err != nil {
			continue
		}
		var files []object.File
		_ = fileIter.ForEach(func(file *object.File) error {
			files = append(files, *file)
			return nil
		})
		qa.filesCache[ref] = files
	}

	qa.view.SetBranches(refs, 0)
}

func (qa *qytController) SetInputFilePathFilter(s string) {
	log.Println("SetInputFilePathFilter", s)

	qa.view.ClearErrorMessage()

	//obj, err := qa.repo.Object(plumbing.CommitObject, ref.Hash())
	//if err != nil {
	//	return err
	//}
	//
	//qyt.HandleMatchingFiles()
}

func (qa *qytController) SetInputQuery(s string) {
	log.Println("SetInputQuery", s)
}

//func (qa qytApp) Run(repo *git.Repository) func() {
//	var (
//		expParser = yqlib.NewExpressionParser()
//		out       = new(bytes.Buffer)
//	)
//
//	refs, exp, fileFilter, initialErr := qa.loadInitialData(repo, expParser)
//	if initialErr != nil {
//		qa.errMessage.SetText(initialErr.Error())
//		qa.errMessage.Show()
//	}
//
//eventLoop:
//	for {
//		out.Reset()
//
//		select {
//		case <-qa.copyRequestC:
//			qa.copyToClipboard()
//			continue eventLoop
//		case b := <-qa.branchC:
//			rs, err := qyt.MatchingBranches(b, repo, false)
//			if err != nil {
//				qa.errMessage.SetText(err.Error())
//				qa.errMessage.Show()
//				continue eventLoop
//			}
//			refs = rs
//		case p := <-qa.pathC:
//			ff, err := regexp.Compile(p)
//			if err != nil {
//				qa.errMessage.SetText(err.Error())
//				qa.errMessage.Show()
//				continue eventLoop
//			}
//			fileFilter = ff
//		case q := <-qa.queryC:
//			ex, err := expParser.ParseExpression(q)
//			if err != nil {
//				qa.errMessage.SetText(err.Error())
//				qa.errMessage.Show()
//				continue eventLoop
//			}
//			exp = ex
//		}
//		qa.errMessage.Hide()
//		qa.errMessage.SetText("")
//		err := qa.runQuery(refs, repo, fileFilter, exp)
//		if err != nil {
//			qa.errMessage.SetText(err.Error())
//			qa.errMessage.Show()
//			continue eventLoop
//		}
//	}
//}
//
//func (qa qytApp) loadInitialData(repo *git.Repository, expParser yqlib.ExpressionParser) ([]plumbing.Reference, *yqlib.ExpressionNode, *regexp.Regexp, error) {
//	exp, err := expParser.ParseExpression(defaultFieldYQExpression())
//	if err != nil {
//		return nil, nil, nil, err
//	}
//	fileFilter, err := regexp.Compile(defaultFieldFileFilter())
//	if err != nil {
//		return nil, nil, nil, err
//	}
//	refs, err := qyt.MatchingBranches(defaultFieldBranchRegex(), repo, false)
//	if err != nil {
//		return nil, exp, fileFilter, err
//	}
//	err = qa.runQuery(refs, repo, fileFilter, exp)
//	if err != nil {
//		return refs, exp, fileFilter, err
//	}
//	return refs, exp, fileFilter, nil
//}
//
//func (qa qytApp) runQuery(references []plumbing.Reference, repo *git.Repository, fileNameMatcher *regexp.Regexp, queryExp *yqlib.ExpressionNode) error {
//	qa.branchTabs.SetItems(nil)
//	buf := new(bytes.Buffer)
//	for _, ref := range references {
//		resultView := container.NewAppTabs()
//		bt := container.NewTabItem(ref.Name().Short(), resultView)
//		resultView.SetTabLocation(container.TabLocationLeading)
//		qa.branchTabs.Append(bt)
//
//		var obj object.Object
//		obj, err := repo.Object(plumbing.CommitObject, ref.Hash())
//		if err != nil {
//			return err
//		}
//		count := 0
//		err = qyt.HandleMatchingFiles(obj, fileNameMatcher, func(file *object.File) error {
//			count++
//			rc, _ := file.Reader()
//			defer func() {
//				_ = rc.Close()
//			}()
//			buf.Reset()
//			err := qyt.ApplyExpression(buf, rc, queryExp, file.Name, qyt.NewScope(ref, file), false)
//			if err != nil {
//				return err
//			}
//			toolbar := widget.NewToolbar()
//			toolbar.Append(widget.NewToolbarAction(theme.ContentCopyIcon(), qa.triggerCopyToClipboard))
//			contents := widget.NewRichTextWithText(buf.String())
//			contents.Wrapping = fyne.TextWrapOff
//			box := container.NewVBox(toolbar, contents)
//			box.Layout.Layout(box.Objects, fyne.NewSize(300, 400))
//			resultView.Append(container.NewTabItem(file.Name, box))
//			return nil
//		})
//		if count == 0 {
//			return fmt.Errorf("no matching files for ref %s", ref.Name())
//		}
//		if err != nil {
//			return err
//		}
//	}
//	return nil
//}
//
//func (qa qytApp) triggerCopyToClipboard() {
//	qa.copyRequestC <- struct{}{}
//}
//
//func (qa qytApp) copyToClipboard() {
//	branchTab := qa.branchTabs.Selected()
//	appTabs, ok := branchTab.Content.(*container.AppTabs)
//	if !ok {
//		return
//	}
//	fileWigetContainer, ok := appTabs.Selected().Content.(*fyne.Container)
//	if !ok || len(fileWigetContainer.Objects) <= 1 {
//		return
//	}
//	rt, ok := fileWigetContainer.Objects[1].(*widget.RichText)
//	if !ok {
//		return
//	}
//	qa.window.Clipboard().SetContent(rt.String())
//}
