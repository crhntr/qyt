package main

import (
	"fmt"
	"github.com/crhntr/qyt/cmd/qyt-webapp/models"
	"github.com/webview/webview"
	"os"
	"reflect"
)

func registerSystemProxyBindings(w webview.WebView) {
	panicOnErr(w.Bind("__fetchAsset", fetchAsset))
	panicOnErr(w.Bind("__writeSync", writeSync))
	panicOnErr(w.Bind("__exit", exit))
}

func fetchAsset(name string) ([]byte, error) {
	mainWASM, err := dir.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return mainWASM, nil
}

func writeSync(fd int, s []byte) {
	switch fd {
	case int(os.Stdout.Fd()):
		os.Stdout.Write(s)
	case int(os.Stderr.Fd()):
		os.Stderr.Write(s)
	}
}

func exit(n int) {
	if n != 0 {
		panic(fmt.Errorf("exit code %d", n))
	}
}

func panicOnResultErr[T any](ret T, err error) T {
	if err != nil {
		panic(err)
	}
	return ret
}

func panicOnErr(err error) {
	if err != nil {
		panic(err)
	}
}

func bindBackend(w webview.WebView, b models.Backend) error {
	return bindPublicMethods(w, b)
}

func bindPublicMethods(w webview.WebView, v interface{}) error {
	value := reflect.ValueOf(v)
	for i := 0; i < value.NumMethod(); i++ {
		mt := value.Type().Method(i)
		if !mt.IsExported() {
			continue
		}
		method := value.Method(i)
		err := w.Bind(mt.Name, method.Interface())
		if err != nil {
			return err
		}
	}
	return nil
}
