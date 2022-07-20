package qyt

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"

	markdown "github.com/MichaelMure/go-term-markdown"

	"github.com/crhntr/qyt/cmd/qyt-webapp/models"
)

type Configuration = models.Configuration

//go:embed README.md
var readme string

func LoadConfiguration(args []string) (Configuration, func(), error) {
	fSet := flag.NewFlagSet("qyt", flag.ContinueOnError)

	var c Configuration

	v := reflect.ValueOf(&c)
	t := v.Elem().Type()

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		envName := f.Tag.Get("env")
		tagDefault := f.Tag.Get("default")

		var (
			usage        = f.Tag.Get("usage")
			defaultValue string
		)
		if envName != "" {
			usage += fmt.Sprintf(" (environment variable %q)", envName)
			defaultValue = os.Getenv(envName)
			if defaultValue == "" {
				defaultValue = tagDefault
			}
		}
		switch v := v.Elem().Field(i).Addr().Interface().(type) {
		case *string:
			fSet.StringVar(v, f.Tag.Get("flag"), defaultValue, usage)
		case *bool:
			dv := defaultValue == "true"
			fSet.BoolVar(v, f.Tag.Get("flag"), dv, usage)
		}
	}

	usage := func() {
		fmt.Print(string(markdown.Render(readme+"\n## Options", 80, 0)))
		fSet.PrintDefaults()
	}
	fSet.Usage = usage

	err := fSet.Parse(args)
	if err != nil {
		return c, usage, err
	}

	if fSet.Arg(0) == "help" {
		return c, usage, errors.New("help requested")
	}

	args = fSet.Args()
	if len(args) > 0 {
		c.Query = args[0]
	}
	if len(args) > 1 {
		c.BranchFilter = args[1]
	}

	return c, usage, nil
}
