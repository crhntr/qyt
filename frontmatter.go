package qyt

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"go.yaml.in/yaml/v4"
)

// FrontmatterNode is a goldmark AST node representing YAML frontmatter.
type FrontmatterNode struct {
	ast.BaseBlock
	BodyStart int
}

// KindFrontmatter is the AST node kind for FrontmatterNode.
var KindFrontmatter = ast.NewNodeKind("Frontmatter")

func (n *FrontmatterNode) Kind() ast.NodeKind { return KindFrontmatter }

func (n *FrontmatterNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// frontmatterParser is a goldmark block parser that detects YAML frontmatter
// at the very start of a markdown document, delimited by --- lines.
type frontmatterParser struct{}

var _ parser.BlockParser = (*frontmatterParser)(nil)

func (p *frontmatterParser) Trigger() []byte { return []byte{'-'} }

func (p *frontmatterParser) Open(_ ast.Node, reader text.Reader, _ parser.Context) (ast.Node, parser.State) {
	lineNum, _ := reader.Position()
	if lineNum != 0 {
		return nil, parser.NoChildren
	}
	line, segment := reader.PeekLine()
	if !bytes.Equal(bytes.TrimRight(line, "\r\n"), []byte("---")) {
		return nil, parser.NoChildren
	}
	reader.Advance(segment.Len())
	return &FrontmatterNode{}, parser.Continue | parser.NoChildren
}

func (p *frontmatterParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	line, segment := reader.PeekLine()
	trimmed := bytes.TrimRight(line, "\r\n")
	if bytes.Equal(trimmed, []byte("---")) || bytes.Equal(trimmed, []byte("...")) {
		node.(*FrontmatterNode).BodyStart = segment.Stop
		reader.Advance(segment.Len())
		return parser.Close
	}
	node.Lines().Append(segment)
	reader.Advance(segment.Len())
	return parser.Continue | parser.NoChildren
}

func (p *frontmatterParser) Close(_ ast.Node, _ text.Reader, _ parser.Context) {}

func (p *frontmatterParser) CanInterruptParagraph() bool  { return false }
func (p *frontmatterParser) CanAcceptIndentedCode() bool  { return false }
func (p *frontmatterParser) CanAcceptIndentedLine() bool  { return false }

// frontmatterExtension extends goldmark with YAML frontmatter parsing.
type frontmatterExtension struct{}

// FrontmatterExtension is a goldmark.Extender that adds support for YAML
// frontmatter delimited by --- at the start of a markdown document.
// YAML content is validated using go.yaml.in/yaml/v4.
var FrontmatterExtension goldmark.Extender = &frontmatterExtension{}

func (e *frontmatterExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithBlockParsers(
		util.Prioritized(&frontmatterParser{}, 1900),
	))
}

var markdownParser = goldmark.New(goldmark.WithExtensions(FrontmatterExtension)).Parser()

// SplitFrontmatter extracts YAML frontmatter from markdown source using goldmark.
// The YAML content is validated with go.yaml.in/yaml/v4.
// It returns the raw frontmatter YAML bytes, the markdown body after the closing ---,
// and whether valid frontmatter was found.
func SplitFrontmatter(source []byte) (frontmatter, body []byte, ok bool) {
	doc := markdownParser.Parse(text.NewReader(source))

	var fmNode *FrontmatterNode
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if fm, isFM := n.(*FrontmatterNode); isFM {
				fmNode = fm
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})

	if fmNode == nil {
		return nil, source, false
	}

	lines := fmNode.Lines()
	for i := range lines.Len() {
		seg := lines.At(i)
		frontmatter = append(frontmatter, seg.Value(source)...)
	}

	var dummy any
	if err := yaml.Unmarshal(frontmatter, &dummy); err != nil {
		return nil, source, false
	}

	return frontmatter, source[fmNode.BodyStart:], true
}

// IsMarkdownFile reports whether filename is a markdown file by extension.
func IsMarkdownFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".md" || ext == ".markdown"
}