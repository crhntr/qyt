package qyt

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"

	markdown "github.com/MichaelMure/go-term-markdown"
)

type Configuration struct {
	Query                    string `env:"QYT_QUERY_EXPRESSION"  flag:"q" default:"keys"         usage:"yq query expression it may be passed argument 1 after flags"`
	BranchFilter             string `env:"QYT_BRANCH_FILTER"     flag:"b" default:".*"           usage:"regular expression to filter branches"`
	FileNameFilter           string `env:"QYT_FILE_NAME_FILTER"  flag:"f" default:"(.+)\\.ya?ml" usage:"regular expression to filter file paths it may be passed argument 2 after flags"`
	GitRepositoryPath        string `env:"QYT_REPO_PATH"         flag:"r" default:"."            usage:"path to git repository"`
	NewBranchPrefix          string `env:"QYT_NEW_BRANCH_PREFIX" flag:"p" default:"qyt/"         usage:"prefix for new branches"`
	CommitToExistingBranches bool   `                            flag:"o" default:"false"        usage:"commit to existing branches instead of new branches"`
	CommitTemplate           string `env:"QYT_COMMIT_TEMPLATE"   flag:"m" default:"run yq {{printf \"%q\" .Query}} on {{.Branch}}" usage:"commit message template"`
}

//go:embed README.md
var readme string

func LoadConfiguration() (Configuration, func(), error) {
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

	err := fSet.Parse(os.Args[1:])
	if err != nil {
		return c, usage, err
	}

	if fSet.Arg(0) == "help" {
		return c, usage, errors.New("help requested")
	}

	args := fSet.Args()
	if len(args) > 0 {
		c.Query = args[0]
	}
	if len(args) > 1 {
		c.BranchFilter = args[1]
	}

	return c, usage, nil
}
