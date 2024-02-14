package main

import (
	"fmt"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/crhntr/qyt"
)

func TestInitialPageCreated(t *testing.T) {
	gitStore, gitFS := memory.NewStorage(), memfs.New()
	repo, _ := git.Init(gitStore, gitFS)
	writeYAML(t, repo, "main.yml", "name: Christopher\n")
	commit(t, repo, "add name")

	qa := initApp(test.NewApp, qyt.Configuration{}, repo)
	qa.loadRepo = func(configuration qyt.Configuration) (*git.Repository, error) {
		return git.Init(gitStore, gitFS)
	}

	go qa.window.ShowAndRun()

	windowClosed := make(chan struct{})
	go func() {
		defer close(windowClosed)
		qa.window.ShowAndRun()
	}()

	queryInput := getFormItemEntryByName(qa.form.Items, formLabelYAMLQuery)
	test.Type(queryInput, ".name")

	submitButton, ok := findObject[*widget.Button](qa.form, func(node *widget.Button) bool {
		return node.Text == formSubmitButtonText
	})
	doneRunning := make(chan struct{})
	go func() {
		defer close(doneRunning)
		for !submitButton.Disabled() {
		}
		for submitButton.Disabled() {
		}
	}()
	test.Tap(submitButton)
	<-doneRunning

	_, ok = findObject[*canvas.Text](qa.branchTabs, func(item *canvas.Text) bool {
		return item.Text == "master"
	})
	if !ok {
		t.Fatal("failed to find branch tab")
	}
	_, ok = findObject[*canvas.Text](qa.branchTabs, func(item *canvas.Text) bool {
		return item.Text == "main.yml"
	})
	if !ok {
		t.Fatal("failed to find file tab")
	}
	_, ok = findObject[*widget.RichText](qa.branchTabs, func(item *widget.RichText) bool {
		return item.String() == "Christopher\n"
	})
	if !ok {
		t.Fatal("failed to find result tab")
	}

	qa.Close()
	<-windowClosed
}

func getFormItemByName(fields []*widget.FormItem, name string) *widget.FormItem {
	for _, f := range fields {
		if f.Text != name {
			continue
		}
		return f
	}
	return nil
}

func getFormItemEntryByName(fields []*widget.FormItem, name string) *widget.Entry {
	field := getFormItemByName(fields, name)
	if field == nil {
		return nil
	}
	return field.Widget.(*widget.Entry)
}

//func getFormItemCheckboxByName(fields []*widget.FormItem, name string) *widget.Check {
//	field := getFormItemByName(fields, name)
//	if field == nil {
//		return nil
//	}
//	return field.Widget.(*widget.Check)
//}

func writeYAML(t *testing.T, repo *git.Repository, fileName, fileContent string) {
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	f, err := wt.Filesystem.Create(fileName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(f, fileContent)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = wt.Add(fileName)
	if err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, repo *git.Repository, message string) {
	sig := object.Signature{
		Name:  "Christopher",
		Email: "chris@example.com",
		When:  time.Unix(783691200, 0),
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	_, err = wt.Commit(message, &git.CommitOptions{
		All:       true,
		Author:    &sig,
		Committer: &sig,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func findObject[CO fyne.CanvasObject](root fyne.CanvasObject, fn func(node CO) bool) (CO, bool) {
	for _, o := range test.LaidOutObjects(root) {
		node, ok := o.(CO)
		if !ok {
			continue
		}
		if fn(node) {
			return node, true
		}
	}
	var zero CO
	return zero, false
}
