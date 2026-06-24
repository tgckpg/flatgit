package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tgckpg/flatgit/internal/config"
	"github.com/tgckpg/flatgit/internal/gitcmd"
)

const maxBlobHTMLBytes = 512 * 1024

type Renderer struct {
	Git        *gitcmd.Runner
	PublicURL  string
	MaxCommits int
}

type RefInfo struct {
	Name   string `json:"name"`
	Short  string `json:"short"`
	Kind   string `json:"kind"`
	Commit string `json:"commit"`
}

type CommitInfo struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Author  string `json:"author"`
	Email   string `json:"email,omitempty"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

type TreeEntry struct {
	Mode string `json:"mode"`
	Type string `json:"type"`
	Hash string `json:"hash"`
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
}

func New(git *gitcmd.Runner, publicURL string, maxCommits int) *Renderer {
	if maxCommits <= 0 {
		maxCommits = 500
	}
	return &Renderer{Git: git, PublicURL: publicURL, MaxCommits: maxCommits}
}

func (r *Renderer) RenderRepo(ctx context.Context, repo config.Repo) error {
	if !isSafeOutput(repo.OutputDir) {
		return fmt.Errorf("unsafe output dir %q", repo.OutputDir)
	}

	commit, err := r.resolveDefaultCommit(ctx, repo)
	if err != nil {
		return err
	}
	refs, err := r.refs(ctx, repo)
	if err != nil {
		return err
	}
	commits, err := r.commits(ctx, repo, commit)
	if err != nil {
		return err
	}
	tree, err := r.tree(ctx, repo, commit)
	if err != nil {
		return err
	}

	next := repo.OutputDir + ".next"
	old := repo.OutputDir + ".old"
	_ = os.RemoveAll(next)
	_ = os.RemoveAll(old)
	if err := os.MkdirAll(next, 0o755); err != nil {
		return err
	}

	if err := writeStatic(next); err != nil {
		return err
	}

	manifest := NewManifest(repo, repo.DefaultBranch, commit)
	if err := writeJSON(filepath.Join(next, "manifest.json"), manifest); err != nil {
		return err
	}

	if err := writeJSON(filepath.Join(next, "refs.json"), refs); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(next, "commits.json"), commits); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(next, "tree.json"), tree); err != nil {
		return err
	}

	branchSlug := refSlug(repo.DefaultBranch)

	page := basePage{
		RepoManifest: manifest,
		Ref:          branchSlug,
		GeneratedAt:  manifest.GeneratedAt.Format(time.RFC3339),
	}

	if err := renderTemplate(filepath.Join(next, "index.html"), indexTemplate, struct {
		basePage
		Refs    []RefInfo
		Commits []CommitInfo
		Files   []TreeEntry
	}{page, refs, firstCommits(commits, 20), firstTree(tree, 50)}); err != nil {
		return err
	}
	if err := renderTemplate(filepath.Join(next, "refs.html"), refsTemplate, struct {
		basePage
		Refs []RefInfo
	}{page, refs}); err != nil {
		return err
	}
	if err := renderTemplate(filepath.Join(next, "log.html"), logTemplate, struct {
		basePage
		Commits []CommitInfo
	}{page, commits}); err != nil {
		return err
	}

	if err := r.renderCommitFiles(ctx, repo, next, commit, branchSlug, repo.DefaultBranch, page); err != nil {
		return err
	}

	for _, c := range commits {
		show, err := r.Git.Text(
			ctx,
			repo.MirrorDir,
			"show",
			"--date=iso-strict-local",
			"--stat",
			"--patch",
			"--find-renames",
			"--no-ext-diff",
			"--no-color",
			c.Hash,
		)
		if err != nil {
			return err
		}

		commitPage := page
		commitPage.Ref = c.Hash

		if err := renderTemplate(filepath.Join(next, "commit", c.Hash+".html"), commitTemplate, struct {
			basePage
			Commit CommitInfo
			Show   string
		}{commitPage, c, show}); err != nil {
			return err
		}

		if err := writeJSON(filepath.Join(next, "commit", c.Hash+".json"), c); err != nil {
			return err
		}

		if err := r.renderCommitFiles(ctx, repo, next, c.Hash, c.Hash, c.Hash, page); err != nil {
			return err
		}
	}

	if err := publish(next, repo.OutputDir, old); err != nil {
		return err
	}
	return nil
}

func (r *Renderer) resolveDefaultCommit(ctx context.Context, repo config.Repo) (string, error) {
	candidates := []string{"refs/heads/" + repo.DefaultBranch, repo.DefaultBranch, "HEAD"}
	var last error
	for _, ref := range candidates {
		out, err := r.Git.Text(ctx, repo.MirrorDir, "rev-parse", "--verify", ref)
		if err == nil {
			return strings.TrimSpace(out), nil
		}
		last = err
	}
	return "", last
}

func (r *Renderer) refs(ctx context.Context, repo config.Repo) ([]RefInfo, error) {
	out, err := r.Git.Output(ctx, repo.MirrorDir, "for-each-ref", "--format=%(refname)%00%(objectname)", "refs/heads", "refs/tags")
	if err != nil {
		return nil, err
	}
	var refs []RefInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		kind := "ref"
		short := name
		if strings.HasPrefix(name, "refs/heads/") {
			kind = "head"
			short = strings.TrimPrefix(name, "refs/heads/")
		} else if strings.HasPrefix(name, "refs/tags/") {
			kind = "tag"
			short = strings.TrimPrefix(name, "refs/tags/")
		}
		refs = append(refs, RefInfo{Name: name, Short: short, Kind: kind, Commit: parts[1]})
	}
	return refs, nil
}

func (r *Renderer) commits(ctx context.Context, repo config.Repo, ref string) ([]CommitInfo, error) {
	limit := strconv.Itoa(r.MaxCommits)
	format := "%H%x00%h%x00%an%x00%ae%x00%ad%x00%s"
	out, err := r.Git.Output(ctx, repo.MirrorDir, "log", "--date=iso-strict-local", "-n", limit, "--pretty=format:"+format, ref, "--")
	if err != nil {
		return nil, err
	}
	var commits []CommitInfo
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		p := strings.SplitN(line, "\x00", 6)
		if len(p) != 6 {
			continue
		}
		commits = append(commits, CommitInfo{Hash: p[0], Short: p[1], Author: p[2], Email: p[3], Date: p[4], Subject: p[5]})
	}
	return commits, nil
}

func (r *Renderer) tree(ctx context.Context, repo config.Repo, ref string) ([]TreeEntry, error) {
	out, err := r.Git.Output(ctx, repo.MirrorDir, "ls-tree", "-r", "-z", ref)
	if err != nil {
		return nil, err
	}
	var entries []TreeEntry
	for _, item := range bytes.Split(out, []byte{0}) {
		if len(item) == 0 {
			continue
		}
		before, path, ok := bytes.Cut(item, []byte{'\t'})
		if !ok {
			continue
		}
		fields := strings.Fields(string(before))
		if len(fields) != 3 {
			continue
		}
		entry := TreeEntry{Mode: fields[0], Type: fields[1], Hash: fields[2], Path: string(path)}
		if entry.Type == "blob" {
			if sizeOut, err := r.Git.Text(ctx, repo.MirrorDir, "cat-file", "-s", entry.Hash); err == nil {
				if n, err := strconv.ParseInt(strings.TrimSpace(sizeOut), 10, 64); err == nil {
					entry.Size = n
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

type basePage struct {
	RepoManifest Manifest
	Ref          string
	GeneratedAt  string
}

type blobView struct {
	Path    string
	Size    int64
	RawHref string
	Text    string
	Binary  bool
}

func writeStatic(root string) error {
	css := `body{max-width:1100px;margin:2rem auto;padding:0 1rem;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;line-height:1.45}a{color:inherit}header{border-bottom:1px solid #ddd;margin-bottom:1rem}nav a{margin-right:1rem}.muted{color:#666}.mono,pre,code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}table{border-collapse:collapse;width:100%}td,th{border-bottom:1px solid #eee;padding:.35rem;text-align:left;vertical-align:top}pre{overflow:auto;background:#f6f6f6;padding:1rem;border:1px solid #eee}.pill{border:1px solid #ddd;border-radius:999px;padding:.1rem .45rem;font-size:.85em}`
	return writeFile(filepath.Join(root, "style.css"), []byte(css), 0o644)
}

func renderTemplate(path, text string, data any) error {
	t, err := template.New(filepath.Base(path)).Funcs(template.FuncMap{
		"short": func(s string) string {
			if len(s) > 12 {
				return s[:12]
			}
			return s
		},
		"href": func(parts ...string) string {
			joined := strings.Join(parts, "/")
			return url.PathEscape(joined)
		},
		"blobHref": func(refSlug, p string) string {
			return "blob/" + refSlug + "/" + strings.TrimPrefix(p, "/") + ".html"
		},
	}).Parse(layoutTemplate + text)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := t.ExecuteTemplate(&b, "layout", data); err != nil {
		return err
	}
	return writeFile(path, b.Bytes(), 0o644)
}

func writeJSON(path string, v any) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return writeFile(path, b.Bytes(), 0o644)
}

func writeFile(path string, b []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, mode)
}

func publish(next, current, old string) error {
	_ = os.RemoveAll(old)
	if _, err := os.Stat(current); err == nil {
		if err := os.Rename(current, old); err != nil {
			return err
		}
	}
	if err := os.Rename(next, current); err != nil {
		if _, statErr := os.Stat(old); statErr == nil {
			_ = os.Rename(old, current)
		}
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

func firstCommits(in []CommitInfo, n int) []CommitInfo {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func firstTree(in []TreeEntry, n int) []TreeEntry {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func refSlug(ref string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimPrefix(ref, "refs/tags/")
	ref = strings.ReplaceAll(ref, "/", "__")
	ref = strings.ReplaceAll(ref, "..", "_")
	if ref == "" {
		return "default"
	}
	return ref
}

func relPath(fromDir, to string) string {
	rel, err := filepath.Rel(fromDir, to)
	if err != nil {
		return to
	}
	return filepath.ToSlash(rel)
}

func isSafeOutput(path string) bool {
	clean := filepath.Clean(path)
	return clean != "." && clean != "/" && clean != ""
}
