package main

import (
	"bufio"
	"bytes"
	"container/list"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	markdown "github.com/MichaelMure/go-term-markdown"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"gopkg.in/op/go-logging.v1"
	"gopkg.in/yaml.v3"
)

var defaultBranchRegex = regexp.MustCompile(`^((main)|(rel/\d+\.\d+))$`)

//go:embed README.md
var README string

func init() {
	backend := logging.NewLogBackend(ioutil.Discard, "", 0)
	logging.SetBackend(backend)
}

func main() {
	var (
		repoPath,
		branchesPattern,
		branchPrefix,
		commitMessage string
		allowOverridingExistingBranches, verbose, outputToJSON bool
		// noConfirm bool
	)

	flags := &flag.FlagSet{}
	flags.StringVar(&branchesPattern, "b", defaultBranchRegex.String(), "regular expression for branches")
	flags.StringVar(&repoPath, "r", ".", "path to local git repository")

	flags.StringVar(&branchPrefix, "p", "", "prefix for created branches (recommended when -m is set)")
	flags.StringVar(&commitMessage, "m", "", "commit message template (CommitMessageData is passed when executing the template)")

	flags.BoolVar(&allowOverridingExistingBranches, "c", false, "commit changes onto existing branches")
	flags.BoolVar(&verbose, "v", false, "verbose logging")

	flags.BoolVar(&outputToJSON, "json", false, "format output as json")
	// flags.BoolVar(&noConfirm, "no-confirm", false, "skip commit confirmation")

	flags.Usage = func() {
		fmt.Print(string(markdown.Render(README+"\n## Options", 80, 0)))
		flags.PrintDefaults()
	}

	err := flags.Parse(os.Args[1:])
	if err != nil {
		fmt.Println("could not parse flags", err)
		flags.Usage()
		os.Exit(1)
	}

	if flags.NArg() < 2 {
		fmt.Println("missing required argument(s)")
		flags.Usage()
		os.Exit(1)
	}

	yqExpressionString := flags.Arg(0)
	filePattern := flags.Arg(1)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to open repository", err)
		os.Exit(1)
	}

	if commitMessage != "" {
		err = Apply(repo, yqExpressionString, branchesPattern, filePattern, commitMessage, branchPrefix, verbose, allowOverridingExistingBranches)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "apply error: %s\n", err.Error())
			os.Exit(1)
		}
		return
	}

	err = Query(os.Stdout, repo, yqExpressionString, branchesPattern, filePattern, verbose, outputToJSON)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "query error: %s\n", err.Error())
		os.Exit(1)
	}
}

func branchesMatchingRegex(branchPattern string, repo *git.Repository, verbose bool) ([]plumbing.Reference, error) {
	var branches []plumbing.Reference

	branchExp, err := regexp.Compile(branchPattern)
	if err != nil {
		return nil, fmt.Errorf("could not complile branch regular expression: %w", err)
	}
	branchIter, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("faild to get branch iterator: %w", err)
	}
	_ = branchIter.ForEach(func(reference *plumbing.Reference) error {
		if branchExp.MatchString(reference.Name().Short()) {
			branches = append(branches, *reference)
		}
		return nil
	})

	if verbose {
		if len(branches) == 1 {
			fmt.Printf("# 1 branch matches regular expression %q\n", branchPattern)
		} else {
			fmt.Printf("# %d branches match regular expression %q\n", len(branches), branchPattern)
		}
	}

	return branches, nil
}

type CommitMessageData struct {
	Branch string
	Query  string
}

func Query(out io.Writer, repo *git.Repository, yqExp, branchRegex, filePattern string, verbose, outputToJSON bool) error {
	parser := yqlib.NewExpressionParser()
	yqExpression, err := parser.ParseExpression(yqExp)
	if err != nil {
		return fmt.Errorf("failed to parse yq expression: %s\n", err)
	}

	branches, err := branchesMatchingRegex(branchRegex, repo, verbose)
	if err != nil {
		return fmt.Errorf("failed to match branches: %s\n", err)
	}

	return query(out, repo, yqExpression, branches, filePattern, verbose, outputToJSON)
}

func query(out io.Writer, repo *git.Repository, exp *yqlib.ExpressionNode, branches []plumbing.Reference, filePattern string, verbose, outputToJSON bool) error {
	for _, branch := range branches {
		if verbose {
			_, _ = fmt.Fprintf(out, "# \tquerying files on %q\n", branch.Name().Short())
		}

		obj, objectErr := repo.Object(plumbing.AnyObject, branch.Hash())
		if objectErr != nil {
			return objectErr
		}

		resolveMatchesErr := resolveMatchingFiles(obj, filePattern, func(file *object.File) error {
			if verbose {
				_, _ = fmt.Fprintf(out, "# \t\tmatched %q\n", file.Name)
			}

			rc, readerErr := file.Reader()
			if readerErr != nil {
				return readerErr
			}

			var buf bytes.Buffer

			applyExpressionErr := applyExpression(&buf, rc, exp, file.Name, map[string]string{
				"branch":   branch.Name().Short(),
				"filename": file.Name,
			}, outputToJSON)
			if applyExpressionErr != nil {
				return fmt.Errorf("could not apply yq operation to file %q on %s: %s", file.Name, branch.Name(), applyExpressionErr)
			}

			_, _ = io.Copy(out, &buf)

			return nil
		})

		if resolveMatchesErr != nil {
			return resolveMatchesErr
		}
	}
	return nil
}

func Apply(repo *git.Repository, yqExp, branchRegex, filePattern, msg, branchPrefix string, verbose, allowOverridingExistingBranches bool) error {
	parser := yqlib.NewExpressionParser()
	yqExpression, err := parser.ParseExpression(yqExp)
	if err != nil {
		return fmt.Errorf("failed to parse yq expression: %s\n", err)
	}

	branches, err := branchesMatchingRegex(branchRegex, repo, verbose)
	if err != nil {
		return fmt.Errorf("failed to match branches: %s\n", err)
	}

	return apply(repo, yqExpression, branches, verbose, allowOverridingExistingBranches, filePattern, msg, branchPrefix, yqExp)
}

func apply(repo *git.Repository, exp *yqlib.ExpressionNode, branches []plumbing.Reference, verbose, allowOverridingExistingBranches bool, filePattern, msg, branchPrefix, expString string) error {
	commitTemplate, templateParseErr := template.New("").Parse(msg)
	if templateParseErr != nil {
		return fmt.Errorf("could not parse commit message template: %w", templateParseErr)
	}

	author, getSignatureErr := getSignature(repo, time.Now())
	if getSignatureErr != nil {
		return getSignatureErr
	}

	var (
		newCommitObjects,
		newBlobObjects,
		newTreeObjects []plumbing.MemoryObject

		newBranches = make(map[plumbing.ReferenceName]plumbing.Hash)
	)

	for _, branch := range branches {
		newBranchName := plumbing.NewBranchReferenceName(branchPrefix + branch.Name().Short())
		if !allowOverridingExistingBranches {
			_, err := repo.Storer.Reference(newBranchName)
			if err == nil {
				return fmt.Errorf("a branch named %q already exists", newBranchName.Short())
			}
		}

		if verbose {
			fmt.Printf("# \tquerying files on %q\n", branch.Name().Short())
		}

		obj, objectErr := repo.Object(plumbing.AnyObject, branch.Hash())
		if objectErr != nil {
			return objectErr
		}

		parentCommit, ok := obj.(*object.Commit)
		if !ok {
			return fmt.Errorf("%s does not point to a commit object: got type %T", branch.Name().Short(), obj)
		}

		updateCount := 0

		var updatedFiles []memoryFile

		resolveMatchesErr := resolveMatchingFiles(obj, filePattern, func(file *object.File) error {
			if verbose {
				fmt.Printf("# \t\tmatched %q\n", file.Name)
			}

			rc, readerErr := file.Reader()
			if readerErr != nil {
				return readerErr
			}
			in, readErr := ioutil.ReadAll(rc)
			if readErr != nil {
				return fmt.Errorf("could not read file %q: %s", file.Name, readErr)
			}

			var out bytes.Buffer

			applyExpressionErr := applyExpression(&out, bytes.NewReader(in), exp, file.Name, map[string]string{
				"branch":   branch.Name().Short(),
				"filename": file.Name,
			}, false)

			if applyExpressionErr != nil {
				return applyExpressionErr
			}

			if bytes.Equal(out.Bytes(), in) {
				if verbose {
					fmt.Printf("# \t\t\tno change\n")
				}
				return nil
			}

			fileObj, saveObjErr := memoryBlobObject(out.Bytes())
			if saveObjErr != nil {
				return saveObjErr
			}

			updatedFiles = append(updatedFiles, memoryFile{
				Name:   filepath.ToSlash(file.Name),
				Mode:   file.Mode,
				Object: fileObj,
			})
			newBlobObjects = append(newBlobObjects, fileObj)

			updateCount++

			return nil
		})
		if resolveMatchesErr != nil {
			return resolveMatchesErr
		}

		if updateCount == 0 {
			continue
		}

		parentTree, treeErr := parentCommit.Tree()
		if treeErr != nil {
			return treeErr
		}

		tree, updatedSubTrees, createTreeErr := createNewTreeWithFiles(parentTree, updatedFiles)
		if createTreeErr != nil {
			return createTreeErr
		}

		for _, subTreeObj := range updatedSubTrees {
			var subTree plumbing.MemoryObject
			treeEncodeErr := subTreeObj.Encode(&subTree)
			if treeEncodeErr != nil {
				return treeEncodeErr
			}
			newTreeObjects = append(newTreeObjects, subTree)
		}

		var treeObj plumbing.MemoryObject
		treeEncodeErr := tree.Encode(&treeObj)
		if treeEncodeErr != nil {
			return treeEncodeErr
		}

		var messageBuf bytes.Buffer
		templateExecErr := commitTemplate.Execute(&messageBuf, CommitMessageData{
			Branch: branch.Name().Short(),
			Query:  expString,
		})
		if templateExecErr != nil {
			return templateExecErr
		}

		commit := object.Commit{
			Author:       author,
			Committer:    author,
			Message:      messageBuf.String(),
			TreeHash:     treeObj.Hash(),
			ParentHashes: []plumbing.Hash{parentCommit.Hash},
		}

		var commitObj plumbing.MemoryObject

		commitEncodeErr := commit.Encode(&commitObj)
		if commitEncodeErr != nil {
			return commitEncodeErr
		}

		newCommitObjects = append(newCommitObjects, commitObj)
		newTreeObjects = append(newTreeObjects, treeObj)
		newBranches[newBranchName] = commitObj.Hash()
	}

	for _, objList := range [][]plumbing.MemoryObject{newBlobObjects, newTreeObjects, newCommitObjects} {
		for _, obj := range objList {
			addObjErr := addObject(repo.Storer, obj)
			if addObjErr != nil {
				return addObjErr
			}
		}
	}

	for name, hash := range newBranches {
		fmt.Println("updating branch", name)

		if !allowOverridingExistingBranches {
			_, err := repo.Storer.Reference(name)
			if err == nil {
				return fmt.Errorf("a branch named %q already exists", name)
			}
		}

		branchRefName := plumbing.NewHashReference(name, hash)

		setRefErr := repo.Storer.SetReference(branchRefName)
		if setRefErr != nil {
			return setRefErr
		}
	}

	return nil
}

type memoryFile struct {
	Name   string
	Mode   filemode.FileMode
	Object plumbing.MemoryObject
}

func createNewTreeWithFiles(parent *object.Tree, files []memoryFile) (*object.Tree, []*object.Tree, error) {
	if parent == nil {
		return nil, nil, nil
	}

	tree := new(object.Tree)

	tree.Entries = make([]object.TreeEntry, len(parent.Entries))
	copy(tree.Entries, parent.Entries)

	var updatedSubTrees []*object.Tree

	for entryIndex, entry := range tree.Entries {
		if ent, ok := createBlobObjectForFile(entry, files); ok {
			tree.Entries[entryIndex] = ent
			continue
		}

		filesForEntry := filterFilesForTreeEntree(entry, files)

		if len(filesForEntry) == 0 {
			continue
		}

		subDir, subTreeErr := parent.Tree(entry.Name)
		if subTreeErr != nil {
			return nil, nil, subTreeErr
		}

		subTree, subTrees, createSubTreeErr := createNewTreeWithFiles(subDir, filesForEntry)
		if createSubTreeErr != nil {
			return nil, nil, createSubTreeErr
		}

		if subTree != nil {
			updatedSubTrees = append(updatedSubTrees, subTree)
			tree.Entries[entryIndex].Hash = subTree.Hash
		}
		if len(subTrees) > 0 {
			updatedSubTrees = append(updatedSubTrees, subTrees...)
		}
	}

	var treeObj plumbing.MemoryObject
	treeEncodeErr := tree.Encode(&treeObj)
	if treeEncodeErr != nil {
		return nil, nil, treeEncodeErr
	}
	treeDecodeErr := tree.Decode(&treeObj)
	if treeDecodeErr != nil {
		return nil, nil, treeDecodeErr
	}

	return tree, updatedSubTrees, nil
}

func createBlobObjectForFile(entry object.TreeEntry, files []memoryFile) (object.TreeEntry, bool) {
	for _, file := range files {
		if file.Name == entry.Name {
			return object.TreeEntry{
				Name: entry.Name,
				Mode: entry.Mode,
				Hash: files[0].Object.Hash(),
			}, true
		}
	}
	return object.TreeEntry{}, false
}

func filterFilesForTreeEntree(entry object.TreeEntry, files []memoryFile) []memoryFile {
	if len(files) == 1 && entry.Name == files[0].Name {
		return []memoryFile{files[0]}
	}

	filesForDir := make([]memoryFile, 0, len(files))

	dirPrefix := entry.Name
	if entry.Mode&filemode.Dir != 0 {
		// I believe forward slash is used by git across windows and *nix.
		dirPrefix += "/"
	}

	for _, file := range files {
		if !strings.HasPrefix(file.Name, dirPrefix) {
			continue
		}
		file.Name = strings.TrimPrefix(file.Name, dirPrefix)
		filesForDir = append(filesForDir, file)
	}

	return filesForDir
}

func addObject(storage storage.Storer, obj plumbing.MemoryObject) (err error) {
	closeErrHandler := func(c io.Closer) {
		closeErr := c.Close()
		if closeErr != nil && err != nil {
			err = closeErr
		}
	}

	_, getObjErr := storage.EncodedObject(obj.Type(), obj.Hash())
	if getObjErr == nil {
		return nil
	}
	storageObj := storage.NewEncodedObject()
	storageObj.SetType(obj.Type())
	storageObj.SetSize(obj.Size())
	wc, writerErr := storageObj.Writer()
	if writerErr != nil {
		return writerErr
	}
	defer closeErrHandler(wc)

	rc, readerErr := obj.Reader()
	if readerErr != nil {
		return readerErr
	}
	defer closeErrHandler(rc)

	_, copyErr := io.Copy(wc, rc)
	if copyErr != nil {
		return copyErr
	}

	_, setObjErr := storage.SetEncodedObject(&obj)
	if setObjErr != nil {
		return setObjErr
	}

	return nil
}

func getSignature(repo *git.Repository, now time.Time) (object.Signature, error) {
	conf, err := repo.ConfigScoped(config.SystemScope)
	if err != nil {
		return object.Signature{}, fmt.Errorf("could not get git config: %w", err)
	}
	if conf.User.Name == "" {
		return object.Signature{}, fmt.Errorf("git user name not set in config")
	}
	if conf.User.Email == "" {
		return object.Signature{}, fmt.Errorf("git user email not set in config")
	}
	return object.Signature{
		Name:  conf.User.Name,
		Email: conf.User.Email,
		When:  now,
	}, nil
}

func memoryBlobObject(buf []byte) (_ plumbing.MemoryObject, err error) {
	var obj plumbing.MemoryObject
	obj.SetType(plumbing.BlobObject)
	obj.SetSize(int64(len(buf)))

	writer, writerErr := obj.Writer()
	if writerErr != nil {
		return plumbing.MemoryObject{}, writerErr
	}
	defer func(c io.Closer) {
		if err != nil {
			return
		}
		err = c.Close()
	}(writer)

	_, copyErr := io.Copy(writer, bytes.NewReader(buf))
	if copyErr != nil {
		return plumbing.MemoryObject{}, copyErr
	}

	return obj, nil
}

func applyExpression(w io.Writer, r io.Reader, exp *yqlib.ExpressionNode, filename string, variables map[string]string, outputToJSON bool) error {
	var bucket yaml.Node
	decoder := yaml.NewDecoder(r)
	err := decoder.Decode(&bucket)
	if err != nil {
		return fmt.Errorf("failed to decode yaml: %s", err)
	}

	navigator := yqlib.NewDataTreeNavigator()

	nodes := list.New()
	nodes.PushBack(&yqlib.CandidateNode{
		Filename:         filename,
		Node:             &bucket,
		EvaluateTogether: true,
	})

	ctx := yqlib.Context{
		MatchingNodes: nodes,
	}
	for k, v := range variables {
		ctx.SetVariable(k, scopeVariable(v))
	}

	result, err := navigator.GetMatchingNodes(ctx, exp)
	if err != nil {
		return fmt.Errorf("yq operation failed: %w", err)
	}

	printer := yqlib.NewPrinter(w, outputToJSON, false, false, 2, true)

	err = printer.PrintResults(result.MatchingNodes)
	if err != nil {
		return fmt.Errorf("rendering result failed: %w", err)
	}

	return nil
}

func scopeVariable(value string) *list.List {
	nodes := list.New()

	var bucket yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(fmt.Sprintf("%q", value)))
	err := decoder.Decode(&bucket)
	if err != nil {
		panic(fmt.Sprintf("failed to decode yaml: %s", err))
	}
	nodes.PushBack(&yqlib.CandidateNode{
		Node: &bucket,
	})

	return nodes
}

func resolveMatchingFiles(obj object.Object, pattern string, fn func(file *object.File) error) error {
	switch o := obj.(type) {
	case *object.Commit:
		t, err := o.Tree()
		if err != nil {
			return err
		}
		return resolveMatchingFiles(t, pattern, fn)
	case *object.Tag:
		target, err := o.Object()
		if err != nil {
			return err
		}
		return resolveMatchingFiles(target, pattern, fn)
	case *object.Tree:
		return o.Files().ForEach(func(file *object.File) error {
			matched, err := filepath.Match(pattern, file.Name)
			if err != nil {
				return err
			}
			if !matched {
				return nil
			}
			return fn(file)
		})
	//case *object.Blob:
	default:
		return object.ErrUnsupportedObject
	}
}

func logCommitMessage(branch plumbing.Reference, obj plumbing.MemoryObject, prefix string) error {
	var commit object.Commit
	commitDecodeErr := commit.Decode(&obj)
	if commitDecodeErr != nil {
		return commitDecodeErr
	}

	fmt.Printf(prefix+"Commiting changes to %s\n", branch.Name().Short())
	r := bufio.NewReader(strings.NewReader(commit.String()))
	for {
		line, readErr := r.ReadString('\n')
		if readErr != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fmt.Printf(prefix+"\t%s\n", line)
	}

	return nil
}
