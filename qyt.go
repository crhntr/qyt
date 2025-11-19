package qyt

import (
	"bytes"
	"container/list"
	_ "embed"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
)

func init() {
	yqlib.InitExpressionParser()
}

func MatchingBranches(branchPattern string, repo *git.Repository, verbose bool) ([]plumbing.Reference, error) {
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
	yqExpression, err := yqlib.ExpressionParser.ParseExpression(yqExp)
	if err != nil {
		return fmt.Errorf("failed to parse yq expression: %s\n", err)
	}

	branches, err := MatchingBranches(branchRegex, repo, verbose)
	if err != nil {
		return fmt.Errorf("failed to match branches: %s\n", err)
	}

	fp, err := regexp.Compile(filePattern)
	if err != nil {
		return fmt.Errorf("failed to parse file name pattern: %s\n", err)
	}

	return query(out, repo, yqExpression, branches, fp, verbose, outputToJSON)
}

func query(out io.Writer, repo *git.Repository, exp *yqlib.ExpressionNode, branches []plumbing.Reference, filePattern *regexp.Regexp, verbose, outputToJSON bool) error {
	for _, branch := range branches {
		if verbose {
			_, _ = fmt.Fprintf(out, "# \tquerying files on %q\n", branch.Name().Short())
		}

		obj, objectErr := repo.Object(plumbing.AnyObject, branch.Hash())
		if objectErr != nil {
			return objectErr
		}

		resolveMatchesErr := HandleMatchingFiles(obj, filePattern, func(file *object.File) error {
			if verbose {
				_, _ = fmt.Fprintf(out, "# \t\tmatched %q\n", file.Name)
			}

			rc, readerErr := file.Reader()
			if readerErr != nil {
				return readerErr
			}
			defer func() {
				_ = rc.Close()
			}()

			var buf bytes.Buffer

			applyExpressionErr := ApplyExpression(&buf, rc, exp, file.Name, map[string]string{
				"branch":   branch.Name().Short(),
				"filename": file.Name,
				"head":     branch.Hash().String(),
			}, outputToJSON)
			if applyExpressionErr != nil {
				return fmt.Errorf("could not apply yq operation to file %q on %s: %s", file.Name, branch.Name(), applyExpressionErr)
			}

			_, err := io.Copy(out, &buf)
			return err
		})

		if resolveMatchesErr != nil {
			return resolveMatchesErr
		}
	}
	return nil
}

func Apply(repo *git.Repository, yqExp, branchRegex, filePattern, msg, branchPrefix string, author object.Signature, verbose, allowOverridingExistingBranches bool) error {
	yqExpression, err := yqlib.ExpressionParser.ParseExpression(yqExp)
	if err != nil {
		return fmt.Errorf("failed to parse yq expression: %s\n", err)
	}

	branches, err := MatchingBranches(branchRegex, repo, verbose)
	if err != nil {
		return fmt.Errorf("failed to match branches: %s\n", err)
	}

	fp, err := regexp.Compile(filePattern)
	if err != nil {
		return fmt.Errorf("failed to parse file name pattern: %s\n", err)
	}

	return apply(repo, yqExpression, branches, author, verbose, allowOverridingExistingBranches, fp, msg, branchPrefix, yqExp)
}

func apply(repo *git.Repository, exp *yqlib.ExpressionNode, branches []plumbing.Reference, author object.Signature, verbose, allowOverridingExistingBranches bool, filePattern *regexp.Regexp, msg, branchPrefix, expString string) error {
	commitTemplate, templateParseErr := template.New("").Parse(msg)
	if templateParseErr != nil {
		return fmt.Errorf("could not parse commit message template: %w", templateParseErr)
	}

	var (
		newCommitObjects,
		newBlobObjects,
		newTreeObjects []plumbing.MemoryObject

		newBranches = make(map[plumbing.ReferenceName]plumbing.Hash)
	)

	for _, branch := range branches {
		newBranchName := plumbing.NewBranchReferenceName(branchPrefix + branch.Name().Short())

		commitObj, blobObjects, treeObjects, applyOnBranchErr := applyOnBranch(
			repo, branch, newBranchName,
			exp, commitTemplate, author,
			expString, filePattern,
			allowOverridingExistingBranches, verbose)

		if applyOnBranchErr != nil {
			return applyOnBranchErr
		}

		if commitObj.Hash().IsZero() {
			continue
		}

		newCommitObjects = append(newCommitObjects, commitObj)
		newBranches[newBranchName] = commitObj.Hash()
		newBlobObjects = append(newBlobObjects, blobObjects...)
		newTreeObjects = append(newTreeObjects, treeObjects...)
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
		if verbose {
			fmt.Println("updating branch", name)
		}

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

func NewScope(branch plumbing.Reference, file *object.File) map[string]string {
	return map[string]string{
		"branch":   branch.Name().Short(),
		"filename": file.Name,
		"head":     branch.Hash().String(),
	}
}

func applyOnBranch(
	repo *git.Repository, branch plumbing.Reference, newBranchName plumbing.ReferenceName,
	exp *yqlib.ExpressionNode,
	commitTemplate *template.Template,
	author object.Signature,
	expString string, filePattern *regexp.Regexp,
	allowOverridingExistingBranches, verbose bool,
) (
	plumbing.MemoryObject, []plumbing.MemoryObject, []plumbing.MemoryObject, error,
) {
	if !allowOverridingExistingBranches {
		_, err := repo.Storer.Reference(newBranchName)
		if err == nil {
			return plumbing.MemoryObject{}, nil, nil,
				fmt.Errorf("a branch named %q already exists", newBranchName.Short())
		}
	}

	if verbose {
		fmt.Printf("# \tquerying files on %q\n", branch.Name().Short())
	}

	obj, objectErr := repo.Object(plumbing.AnyObject, branch.Hash())
	if objectErr != nil {
		return plumbing.MemoryObject{}, nil, nil, objectErr
	}

	parentCommit, ok := obj.(*object.Commit)
	if !ok {
		return plumbing.MemoryObject{}, nil, nil,
			fmt.Errorf("%s does not point to a commit object: got type %T", branch.Name().Short(), obj)
	}

	updateCount := 0

	var (
		updatedFiles []memoryFile
		newBlobObjects,
		newTreeObjects []plumbing.MemoryObject
	)

	resolveMatchesErr := HandleMatchingFiles(obj, filePattern, func(file *object.File) error {
		if verbose {
			fmt.Printf("# \t\tmatched %q\n", file.Name)
		}

		rc, readerErr := file.Reader()
		if readerErr != nil {
			return readerErr
		}
		in, readErr := io.ReadAll(rc)
		if readErr != nil {
			return fmt.Errorf("could not read file %q: %s", file.Name, readErr)
		}

		var out bytes.Buffer

		applyExpressionErr := ApplyExpression(&out, bytes.NewReader(in), exp, file.Name, NewScope(branch, file), false)

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
		return plumbing.MemoryObject{}, nil, nil, resolveMatchesErr
	}

	if updateCount == 0 {
		return plumbing.MemoryObject{}, nil, nil, nil
	}

	parentTree, treeErr := parentCommit.Tree()
	if treeErr != nil {
		return plumbing.MemoryObject{}, nil, nil, treeErr
	}

	tree, updatedSubTrees, createTreeErr := createNewTreeWithFiles(parentTree, updatedFiles)
	if createTreeErr != nil {
		return plumbing.MemoryObject{}, nil, nil, createTreeErr
	}

	for _, subTreeObj := range updatedSubTrees {
		var subTree plumbing.MemoryObject
		treeEncodeErr := subTreeObj.Encode(&subTree)
		if treeEncodeErr != nil {
			return plumbing.MemoryObject{}, nil, nil, treeEncodeErr
		}
		newTreeObjects = append(newTreeObjects, subTree)
	}

	var treeObj plumbing.MemoryObject
	treeEncodeErr := tree.Encode(&treeObj)
	if treeEncodeErr != nil {
		return plumbing.MemoryObject{}, nil, nil, treeEncodeErr
	}

	var messageBuf bytes.Buffer
	templateExecErr := commitTemplate.Execute(&messageBuf, CommitMessageData{
		Branch: branch.Name().Short(),
		Query:  expString,
	})
	if templateExecErr != nil {
		return plumbing.MemoryObject{}, nil, nil, templateExecErr
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
		return plumbing.MemoryObject{}, nil, nil, commitEncodeErr
	}

	newTreeObjects = append(newTreeObjects, treeObj)

	return commitObj, newBlobObjects, newTreeObjects, nil
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

func ApplyExpression(w io.Writer, r io.Reader, exp *yqlib.ExpressionNode, filename string, variables map[string]string, outputToJSON bool) error {
	nodes := list.New()

	decoder := yqlib.NewYamlDecoder(yqlib.NewDefaultYamlPreferences())
	if err := decoder.Init(r); err != nil {
		return err
	}
	candidateNode, err := decoder.Decode()
	if err != nil {
		return err
	}
	candidateNode.SetFilename(filename)
	candidateNode.EvaluateTogether = true

	navigator := yqlib.NewDataTreeNavigator()
	nodes.PushBack(candidateNode)

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

	var encoder yqlib.Encoder
	if outputToJSON {
		encoder = yqlib.NewJSONEncoder(yqlib.JsonPreferences{
			Indent:       2,
			UnwrapScalar: true,
		})
	} else {
		encoder = yqlib.NewYamlEncoder(yqlib.YamlPreferences{
			Indent:             2,
			PrintDocSeparators: true,
			UnwrapScalar:       true,
		})
	}
	printerWriter := yqlib.NewSinglePrinterWriter(w)
	printer := yqlib.NewPrinter(encoder, printerWriter)

	err = printer.PrintResults(result.MatchingNodes)
	if err != nil {
		return fmt.Errorf("rendering result failed: %w", err)
	}

	return nil
}

func scopeVariable(value string) *list.List {
	nodes := list.New()

	dec := yqlib.NewYamlDecoder(yqlib.NewDefaultYamlPreferences())
	if err := dec.Init(strings.NewReader(fmt.Sprintf("%q", value))); err != nil {
		panic(fmt.Sprintf("failed to decode yaml: %s", err))
	}
	candidateNode, err := dec.Decode()
	if err != nil {
		panic(fmt.Sprintf("failed to decode yaml: %s", err))
	}

	nodes.PushBack(candidateNode)

	return nodes
}

func HandleMatchingFiles(obj object.Object, re *regexp.Regexp, fn func(file *object.File) error) error {
	switch o := obj.(type) {
	case *object.Commit:
		t, err := o.Tree()
		if err != nil {
			return err
		}
		return HandleMatchingFiles(t, re, fn)
	case *object.Tag:
		target, err := o.Object()
		if err != nil {
			return err
		}
		return HandleMatchingFiles(target, re, fn)
	case *object.Tree:
		return o.Files().ForEach(func(file *object.File) error {
			if re != nil {
				if !re.MatchString(file.Name) {
					return nil
				}
			}
			return fn(file)
		})
	// case *object.Blob:
	default:
		return object.ErrUnsupportedObject
	}
}
