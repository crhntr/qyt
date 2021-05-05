qyt [options] <yq_expression> <file pattern>

Run yq queries across multiple git branches and files.
See https://mikefarah.gitbook.io/yq/ for query syntax documentation.

## Queries

  Example: Read the name value from every yaml file.

    qyt '.name' '*.yml'

  Example: Display the top level keys for each file on each branch.

    qyt '{"b": $branch, "fp": filename, "keys": keys}' '*.yml'

## Writing commit messages

  When you set an optional commit message template, the result of the
  yq operation will be committed to each branch. The commit message
  is executed as a template with a populated "CommitMessageData" data
  structure.

    type CommitMessageData struct {
      Branch string
      Query  string
    }

  For example when you run the following command (with branches main, and rel/2.0)

    qyt -m 'run {{printf "%q" .Query}} on {{.Branch}}' 'del(.name)' data.yml

  The commit messages will look like

    run "del(.name)" on main

  and

    run "del(.name)" on rel/2.0

  For more about Go text/templates see: https://golang.org/pkg/text/template/

