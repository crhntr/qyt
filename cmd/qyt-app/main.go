package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/crhntr/qyt"
)

//go:embed file_view.md
var fileViewMD string

func main() {
	myApp := app.New()
	mainWindow := myApp.NewWindow("TabContainer Widget")
	mainWindow.Resize(fyne.NewSize(800, 600))

	repo, err := git.PlainOpen("config")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", err)
		os.Exit(1)
	}

	form := widget.NewForm()
	branchQuery := widget.NewEntry()
	form.Append("Branch Query", branchQuery)
	yqExpression := widget.NewEntry()
	form.Append("YQ Expression", yqExpression)

	tabs := container.NewAppTabs()
	branchQuery.OnSubmitted = func(branchQueryExpression string) {
		if len(tabs.Items) > 0 {
			tabs.SetItems(nil)
		}
		branches, err := qyt.MatchingBranches(branchQueryExpression, repo, false)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", err)
			os.Exit(1)
		}
		for _, ref := range branches {
			v, err := repoCanvasObject(repo, ref)
			if err != nil {
				panic(err)
			}
			bt := container.NewTabItem(ref.Name().Short(), v)
			tabs.Append(bt)
		}
		tabs.SetTabLocation(container.TabLocationTop)
	}
	yqExpression.OnSubmitted = func(exp string) {

	}

	mainView := container.NewVSplit(
		form,
		tabs,
	)

	mainWindow.SetContent(mainView)
	mainWindow.ShowAndRun()
}

func repoCanvasObject(repo *git.Repository, ref plumbing.Reference) (fyne.CanvasObject, error) {
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}
	tree, err := repo.TreeObject(commit.TreeHash)
	if err != nil {
		return nil, err
	}
	rootNode := parseNodes(tree)

	treeView := widget.NewTree(
		func(id widget.TreeNodeID) []widget.TreeNodeID {
			c, _ := rootNode.find(id)
			names := c.childNames()
			return names
		},
		func(id widget.TreeNodeID) bool {
			c, ok := rootNode.find(id)
			if !ok {
				return false
			}
			return c.isBranch()
		},
		func(b bool) fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TreeNodeID, b bool, canvasObject fyne.CanvasObject) {
			canvasObject.(*widget.Label).SetText(fmt.Sprintf("%s", path.Base(id)))
		},
	)

	textView := widget.NewRichTextFromMarkdown("")

	treeView.OnSelected = func(uid widget.TreeNodeID) {
		n, ok := rootNode.find(uid)
		if !ok || n.isBranch() {
			return
		}
		f, err := tree.File(uid)
		if err != nil {
			return
		}
		rc, _ := f.Reader()
		defer func() {
			_ = rc.Close()
		}()
		buf, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
		textView.ParseMarkdown("---\n```yml" + string(buf) + "\n```\n")
	}

	return container.NewHSplit(treeView, textView), nil
}

type node struct {
	Path     string
	Children []node
}

func (d node) name() string {
	return path.Base(d.Path)
}

func (d node) isBranch() bool {
	return len(d.Children) > 0
}

func (d node) find(s string) (node, bool) {
	if d.Path == s {
		return d, true
	}
	for _, c := range d.Children {
		sepIndex := strings.IndexByte(s, '/')
		if sepIndex < 0 && c.Path == s {
			return c, true
		}

		if sepIndex >= 0 && sepIndex <= len(c.Path) {
			if s[:sepIndex] == c.Path[:sepIndex] {
				return c.find(s)
			}
		}
	}
	return node{}, false
}

func (d *node) addPrefix(s string) {
	for i := range d.Children {
		d.Children[i].Path = path.Join(s, d.Children[i].Path)
		d.Children[i].addPrefix(s)
	}
}

func (d node) childNames() []string {
	if len(d.Children) == 0 {
		return nil
	}
	result := make([]string, 0, len(d.Children))
	for _, c := range d.Children {
		result = append(result, c.Path)
	}
	return result
}

func parseNodes(tree *object.Tree) node {
	var n node
	for _, entry := range tree.Entries {
		t, err := tree.Tree(entry.Name)
		if err != nil {
			n.Children = append(n.Children, node{Path: entry.Name})
			continue
		}
		d := parseNodes(t)
		d.Path = entry.Name
		d.addPrefix(entry.Name)
		n.Children = append(n.Children, d)
	}
	return n
}
