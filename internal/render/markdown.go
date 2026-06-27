package render

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

func renderMarkdownSafe(src []byte) ([]byte, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)

	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return nil, err
	}

	policy := bluemonday.UGCPolicy()
	return policy.SanitizeBytes(buf.Bytes()), nil
}

func isMarkdownPath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))

	return ext == ".md" || name == "readme"
}

func findReadmePath(tree []TreeEntry) string {
	for _, e := range tree {
		if e.Type != "blob" {
			continue
		}

		name := strings.ToLower(e.Path)

		switch name {
		case "readme.md", "readme.markdown", "readme":
			return e.Path
		}
	}

	return ""
}
