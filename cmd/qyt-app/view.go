package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type view struct {
	c controller

	window fyne.Window
	form   *widget.Form

	errMessage *widget.Label

	branchEntry,
	pathEntry,
	queryEntry *widget.Entry

	branchTabs   *container.AppTabs
	fileTabs     map[plumbing.Reference]*container.AppTabs
	fileContents *widget.RichText
}

func (v view) Window() fyne.Window {
	return v.window
}

type windowInitializer interface {
	NewWindow(string) fyne.Window
}

type controller interface {
	SetSelectedBranch(i int)
	SetSelectedFile(ref plumbing.Reference, i int)

	SetInputBranchFilter(s string)
	SetInputFilePathFilter(s string)
	SetInputQuery(s string)
}

func createView(initializer windowInitializer, c controller) view {
	mainWindow := initializer.NewWindow("qyt = yq * git")
	mainWindow.Resize(fyne.NewSize(800, 600))

	v := view{
		c:      c,
		window: mainWindow,
		form:   widget.NewForm(),

		errMessage: widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),

		branchEntry: widget.NewEntry(),
		pathEntry:   widget.NewEntry(),
		queryEntry:  widget.NewEntry(),

		branchTabs:   container.NewAppTabs(),
		fileTabs:     make(map[plumbing.Reference]*container.AppTabs),
		fileContents: widget.NewRichText(),
	}

	v.form.Append("YAML Query", v.queryEntry)
	v.form.Append("Branch RegExp", v.branchEntry)
	v.form.Append("File RegExp", v.pathEntry)

	v.branchEntry.OnSubmitted = v.submitChangeBranchFilter
	v.pathEntry.OnSubmitted = v.submitChangeFilePathFilter
	v.queryEntry.OnSubmitted = v.submitChangeQuery
	v.branchTabs.OnSelected = v.branchSelected

	v.branchTabs.Append(container.NewTabItem("hello", widget.NewLabel("world")))

	mainWindow.SetContent(container.NewVSplit(container.NewVBox(v.form, v.errMessage), v.branchTabs))

	return v
}

func (v *view) DisableForm() {
	v.form.Disable()
}

func (v *view) EnableForm() {
	v.form.Enable()
}

func (v *view) SetBranchInputValue(s string) {
	v.branchEntry.SetText(s)
	v.c.SetInputBranchFilter(s)
}
func (v *view) SetPathInputValue(s string) {
	v.pathEntry.SetText(s)
	v.c.SetInputBranchFilter(s)
}
func (v *view) SetQueryInputValue(s string) {
	v.queryEntry.SetText(s)
	v.c.SetInputQuery(s)
}

func (v *view) SetErrorMessage(message string) {
	v.errMessage.SetText(message)
	v.errMessage.Show()
}

func (v *view) ClearErrorMessage() {
	v.errMessage.Hide()
	v.errMessage.SetText("")
}

func (v *view) SetBranches(refs []plumbing.Reference, selectIndex int) {
	if len(refs) == 0 {
		v.branchTabs.SetItems(nil)
		return
	}
	for k := range v.fileTabs {
		delete(v.fileTabs, k)
	}
	tabs := make([]*container.TabItem, len(refs))
	for i, ref := range refs {
		fileTabs := container.NewAppTabs()
		fileTabs.SetTabLocation(container.TabLocationLeading)
		fileTabs.OnSelected = v.fileSelected(fileTabs, ref)
		tabs[i] = container.NewTabItem(ref.Name().Short(), fileTabs)
		v.fileTabs[ref] = fileTabs
	}
	v.branchTabs.SetItems(tabs)
	v.branchTabs.SelectIndex(selectIndex)
	v.branchTabs.Show()
}

func (v *view) SetFiles(ref plumbing.Reference, files []object.File, selectIndex int) {
	appTabs := v.fileTabs[ref]
	if len(files) == 0 {
		if appTabs != nil {
			appTabs.Items = nil
			appTabs.Refresh()
		}
		return
	}
	if len(appTabs.Items) > 0 {
		appTabs.SelectIndex(selectIndex)
		return
	}
	tabItems := make([]*container.TabItem, len(files))
	for i, file := range files {
		tabItems[i] = container.NewTabItem(file.Name, v.fileContents)
	}
	appTabs.SetItems(tabItems)
	appTabs.SelectIndex(selectIndex)
	appTabs.Show()
}

func (v *view) SetFileContent(content string) {
	temporary := widget.NewRichTextWithText(content)
	v.fileContents.Segments = temporary.Segments
	v.fileContents.Refresh()
	v.fileContents.Show()
}

func (v *view) branchSelected(*container.TabItem) {
	v.fileContents.Hide()

	i := v.branchTabs.SelectedIndex()
	v.c.SetSelectedBranch(i)
}

func (v *view) fileSelected(fileTabs *container.AppTabs, ref plumbing.Reference) func(tab *container.TabItem) {
	return func(tab *container.TabItem) {
		v.fileContents.Hide()
		v.c.SetSelectedFile(ref, fileTabs.SelectedIndex())
	}
}

func (v *view) submitChangeBranchFilter(s string) {
	v.branchTabs.Hide()
	v.fileContents.Hide()
	v.c.SetInputBranchFilter(s)
}

func (v *view) submitChangeFilePathFilter(s string) {
	v.fileContents.Hide()
	v.c.SetInputFilePathFilter(s)
}

func (v *view) submitChangeQuery(s string) {
	v.fileContents.Hide()
	v.c.SetInputQuery(s)
}
