package main

import (
	"bytes"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
)

func TestCreateNewTreeWithFiles(t *testing.T) {
	t.Run("nil inputs", func(t *testing.T) {
		assert.NotPanics(t, func() {
			updated, _, _ := createNewTreeWithFiles(nil, nil)
			assert.Nil(t, updated)
		})
	})

	t.Run("nil files", func(t *testing.T) {
		assert.NotPanics(t, func() {
			_, _, _ = createNewTreeWithFiles(&object.Tree{}, nil)
		})
	})

	t.Run("single file in sub directory", func(t *testing.T) {
		e := object.TreeEntry{
			Name: "dir",
			Mode: filemode.Dir,
		}
		input := []memoryFile{
			{Name: "skip1.txt"},
			{Name: "dir", Mode: filemode.Dir | 0777},
			{Name: "dir/file.txt"},
			{Name: "skip2.txt"},
		}

		filtered := filterFilesForTreeEntree(e, input)

		assert.Len(t, filtered, 1)
		for _, f := range filtered {
			assert.Equal(t, "file.txt", f.Name)
		}
	})

	t.Run("multiple directories with the same prefix", func(t *testing.T) {
		e := object.TreeEntry{
			Name: "dir",
			Mode: filemode.Dir,
		}
		input := []memoryFile{
			{Name: "skip1.txt"},
			{Name: "dir_skip", Mode: filemode.Dir | 0777},
			{Name: "dir_skip/file.txt"},
			{Name: "dir", Mode: filemode.Dir | 0777},
			{Name: "dir/file.txt"},
			{Name: "skip2.txt"},
		}

		filtered := filterFilesForTreeEntree(e, input)

		assert.Len(t, filtered, 1)
		for _, f := range filtered {
			assert.Equal(t, "file.txt", f.Name)
		}
	})

	t.Run("multiple embedded sub directories", func(t *testing.T) {
		e := object.TreeEntry{
			Name: "dir",
			Mode: filemode.Dir,
		}
		input := []memoryFile{
			{Name: "skip1.txt"},
			{Name: "dir_skip", Mode: filemode.Dir | 0777},
			{Name: "dir_skip/file.txt"},
			{Name: "dir", Mode: filemode.Dir | 0777},
			{Name: "dir/file.txt"},
			{Name: "skip2.txt"},
		}

		filtered := filterFilesForTreeEntree(e, input)

		assert.Len(t, filtered, 1)
		for _, f := range filtered {
			assert.Equal(t, "file.txt", f.Name)
		}
	})

	t.Run("multiple files in multiple subdirectories directories", func(t *testing.T) {
		e := object.TreeEntry{
			Name: "dir",
			Mode: filemode.Dir,
		}
		input := []memoryFile{
			{Name: "skip1.txt"},
			{Name: "dir_skip", Mode: filemode.Dir | 0777},
			{Name: "dir_skip/file.txt"},
			{Name: "dir", Mode: filemode.Dir | 0777},
			{Name: "dir/file.txt"},
			{Name: "dir/sub1/file.txt"},
			{Name: "dir/sub2/file.txt"},
			{Name: "skip2.txt"},
		}

		filtered := filterFilesForTreeEntree(e, input)

		assert.Len(t, filtered, 3)

		for _, f := range filtered {
			assert.Equal(t, filepath.Base(f.Name), "file.txt")
		}
	})
}

func TestTrueIfYEntered(t *testing.T) {
	t.Run("Y", func(t *testing.T) {
		r := strings.NewReader("Y\n")
		var w bytes.Buffer
		res := trueIfYEntered(&w, r, "input something")
		assert.True(t, res)
		buf, _ := ioutil.ReadAll(&w)
		assert.Equal(t, string(buf), "input something Y/n: ")
	})

	t.Run("not valid y", func(t *testing.T) {
		r := strings.NewReader("foo\nY\n")
		var w bytes.Buffer
		res := trueIfYEntered(&w, r, "input something")
		assert.True(t, res)
		buf, _ := ioutil.ReadAll(&w)
		assert.Equal(t, string(buf), "input something Y/n: input something Y/n: ")
	})

	t.Run("not valid y", func(t *testing.T) {
		r := strings.NewReader("n\n")
		var w bytes.Buffer
		res := trueIfYEntered(&w, r, "input something")
		assert.False(t, res)
		buf, _ := ioutil.ReadAll(&w)
		assert.Equal(t, string(buf), "input something Y/n: ")
	})
}
