# QYT lets you run yq across git branches!

```
  qyt [options] <yq_expression> <file_regexp>
```

See [yq docs](https://mikefarah.gitbook.io/yq/) for query syntax.

## Examples

### Read the name value from every yaml file
```sh
  qyt '.name' '*.yml'
```

### Display the top level keys for each file on each branch

```sh
  qyt '{"b": $branch, "fp": $filename, "keys": keys}' '*.yml'
```

## Committing Query Results

You can update files in each branch by configuring a commit message.
qyu writes the result of the query to the source file and adds all the
modified files to a commit.

The commit message is executed as a template with a populated
"CommitMessageData" data structure.

```go
  type CommitMessageData struct {
    Branch string
    Query  string
  }
```

For example when you run the following command (with branches main, and rel/2.0)

```sh
  qyt -m 'run {{printf "%q" .Query}} on {{.Branch}}' 'del(.name)' data.yml
```

The commit messages will look like
```
  run "del(.name)" on main
```
and
```
run "del(.name)" on rel/2.0
```

See [text/templates](https://golang.org/pkg/text/template/) for template syntax.
