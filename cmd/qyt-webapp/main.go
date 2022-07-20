//go:build js && wasm

package main

import (
	_ "embed"
	"fmt"
	"regexp"
	"syscall/js"

	"github.com/crhntr/window/browser"
	"github.com/crhntr/window/dom"
)

var document = browser.Document(js.Global().Get("document"))

//go:embed embed/main.html
var mainHTML string

func main() {
	backend := setupBackend()

	document.QuerySelector(`body`).SetInnerHTML(mainHTML)

	config, err := backend.InitialConfiguration()
	if err != nil {
		fmt.Println(err)
		return
	}
	var (
		elements     = document.QuerySelectorAll(`#yaml-query, #branch-filter, #file-filter, #commit-result, #commit-message, #create-new-branches, #branch-prefix, #matching-branches`)
		yamlQuery    = browser.Input(elements.Item(0).(browser.Element))
		branchFilter = browser.Input(elements.Item(1).(browser.Element))
		fileFilter   = browser.Input(elements.Item(2).(browser.Element))
		// commitResult = elements.Item(3).(browser.Input)
		//commitMessage     = elements.Item(4).(browser.Element)
		//createNewBranches = elements.Item(5).(browser.Input)
		//branchPrefix      = elements.Item(6).(browser.Input)
		matchingBranches = elements.Item(7).(browser.Element)
	)

	yamlQuery.SetValue(config.Query)
	branchFilter.SetValue(config.BranchFilter)
	fileFilter.SetValue(config.FileNameFilter)

	branchFilterKeyup := browser.NewEventListenerFunc(func(event browser.Event) {
		el, ok := event.Target().(browser.Input)
		if !ok {
			return
		}
		s := el.Value()
		if s == "" {
			return
		}
		filter, err := regexp.Compile(s)
		if err != nil {
			matchingBranches.ReplaceChildren()
			matchingBranches.InsertAdjacentHTML(dom.PositionBeforeEnd, fmt.Sprintf(`<p class="error">%s</p>`, err))
			return
		}
		branches, err := backend.ListBranchNames()
		if err != nil {
			matchingBranches.ReplaceChildren()
			matchingBranches.InsertAdjacentHTML(dom.PositionBeforeEnd, fmt.Sprintf(`<p class="error">%s</p>`, err))
			return
		}
		matchingBranches.ReplaceChildren()
		for _, branch := range branches {
			if filter.MatchString(branch) {
				continue
			}
			matchingBranches.InsertAdjacentHTML(dom.PositionBeforeEnd, fmt.Sprintf(`<div class="match">%s</div>`, branch))
		}
	})
	defer branchFilterKeyup.Release()
	branchFilter.AddEventListener("keyup", branchFilterKeyup)

	select {}
}
