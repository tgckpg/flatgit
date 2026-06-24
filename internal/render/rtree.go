package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/tgckpg/flatgit/internal/config"
)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func linkOrCopy(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	// If this is a re-render into a fresh `next` dir this usually won't exist,
	// but removing makes the helper safe for reuse.
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Link(src, dst); err == nil {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()

	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (r *Renderer) renderCommitFiles(
	ctx context.Context,
	repo config.Repo,
	next string,
	commit string,
	revSlug string,
	refLabel string,
	page basePage,
) error {
	tree, err := r.tree(ctx, repo, commit)
	if err != nil {
		return err
	}

	if err := writeJSON(filepath.Join(next, "tree", revSlug, "tree.json"), tree); err != nil {
		return err
	}

	if err := renderTemplate(filepath.Join(next, "tree", revSlug, "index.html"), treeTemplate, struct {
		basePage
		Ref   string
		Files []TreeEntry
	}{page, refLabel, tree}); err != nil {
		return err
	}

	for _, e := range tree {
		if e.Type != "blob" {
			continue
		}

		if err := r.renderBlobEntry(ctx, repo, next, commit, revSlug, page, e); err != nil {
			return err
		}
	}

	return nil
}

func (r *Renderer) renderBlobEntry(
	ctx context.Context,
	repo config.Repo,
	next string,
	commit string,
	revSlug string,
	page basePage,
	e TreeEntry,
) error {
	if e.Hash == "" {
		return fmt.Errorf("tree entry %q has empty blob hash", e.Path)
	}

	blobID := e.Hash

	rawObjectPath := filepath.Join(
		next,
		"_objects",
		"raw",
		blobID[:2],
		blobID,
	)

	if !fileExists(rawObjectPath) {
		content, err := r.Git.Output(
			ctx,
			repo.MirrorDir,
			"cat-file",
			"-p",
			blobID,
		)
		if err != nil {
			return fmt.Errorf("read blob %s for %s at %s: %w", blobID, e.Path, commit, err)
		}

		if err := writeFile(rawObjectPath, content, 0o644); err != nil {
			return fmt.Errorf("write raw object %s: %w", rawObjectPath, err)
		}
	}

	rawPath := filepath.Join(
		next,
		"raw",
		revSlug,
		filepath.FromSlash(e.Path),
	)

	if err := linkOrCopy(rawObjectPath, rawPath, 0o644); err != nil {
		return fmt.Errorf("link/copy raw %s -> %s: %w", rawObjectPath, rawPath, err)
	}

	content, err := os.ReadFile(rawObjectPath)
	if err != nil {
		return fmt.Errorf("read raw object %s: %w", rawObjectPath, err)
	}

	blobPath := filepath.Join(
		next,
		"blob",
		revSlug,
		filepath.FromSlash(e.Path)+".html",
	)

	blob := blobView{
		Path:    e.Path,
		Size:    int64(len(content)),
		RawHref: relPath(filepath.Dir(blobPath), rawPath),
	}

	if len(content) > maxBlobHTMLBytes || bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
		blob.Binary = true
	} else {
		blob.Text = string(content)
	}

	if err := renderTemplate(blobPath, blobTemplate, struct {
		basePage
		Blob blobView
	}{page, blob}); err != nil {
		return fmt.Errorf("render blob page %s: %w", blobPath, err)
	}

	return nil
}
