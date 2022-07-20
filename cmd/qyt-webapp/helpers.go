package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/crhntr/qyt/cmd/qyt-webapp/models"
)

func setupBackend() models.Backend {
	return &backend{}
}

type backend struct{}

func (b backend) InitialConfiguration() (models.Configuration, error) {
	return invokeNoParameter[models.Configuration]("InitialConfiguration")
}

func (b backend) ListBranchNames() ([]string, error) {
	return invokeNoParameter[[]string]("ListBranchNames")
}

func invokeNoParameter[RT any](name string) (RT, error) {
	var value RT
	fn := js.Global().Get(name)
	if fn.IsUndefined() {
		return value, fmt.Errorf("%s is undefined", name)
	}
	c := make(chan string)
	fn.Invoke().Call("then", js.FuncOf(func(_ js.Value, args []js.Value) any {
		result := args[0]
		jsonEncoder := js.Global().Get("JSON")
		c <- jsonEncoder.Call("stringify", result).String()
		close(c)
		return nil
	}))
	err := json.Unmarshal([]byte(<-c), &value)
	return value, err
}

func invokeOneParameter[RT, PT any](name string, param PT) (RT, error) {
	paramValue := js.ValueOf(param)
	var value RT
	fn := js.Global().Get(name)
	if fn.IsUndefined() {
		return value, fmt.Errorf("%s is undefined", name)
	}
	c := make(chan string)
	fn.Invoke(paramValue).Call("then", js.FuncOf(func(_ js.Value, args []js.Value) any {
		result := args[0]
		jsonEncoder := js.Global().Get("JSON")
		c <- jsonEncoder.Call("stringify", result).String()
		close(c)
		return nil
	}))
	err := json.Unmarshal([]byte(<-c), &value)
	return value, err
}
