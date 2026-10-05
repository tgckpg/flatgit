package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/tgckpg/flatgit/internal/config"
)

type gitHTTP struct {
	gitCommand string
	logger     *slog.Logger

	// Exact HTTP repository base path -> bare mirror directory.
	//
	// Example:
	//   /penguin/flatgit -> /var/lib/flatgit/repos/...
	repos map[string]string
}

func newGitHTTP(
	gitCommand string,
	repos []config.Repo,
	logger *slog.Logger,
) *gitHTTP {
	if gitCommand == "" {
		gitCommand = "git"
	}

	g := &gitHTTP{
		gitCommand: gitCommand,
		logger:     logger,
		repos:      make(map[string]string, len(repos)*2),
	}

	for _, repo := range repos {
		base := strings.TrimSuffix(repo.RepoBase(), "/")

		g.repos[base] = repo.MirrorDir

		// Supporting the conventional .git form is basically free.
		g.repos[base+".git"] = repo.MirrorDir
	}

	return g
}

// ServeHTTP handles only the two endpoints needed for read-only Smart HTTP.
//
// Returns true if the request belonged to the Git transport.
// Everything else should fall through to Flatgit's static server.
func (g *gitHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimSuffix(r.URL.Path, "/")

	/*
		GET /owner/repo/info/refs?service=git-upload-pack
	*/
	if r.Method == http.MethodGet &&
		strings.HasSuffix(path, "/info/refs") {

		base := strings.TrimSuffix(path, "/info/refs")
		mirror, ok := g.repos[base]
		if !ok {
			return false
		}

		if r.URL.Query().Get("service") != "git-upload-pack" {
			http.Error(w, "unsupported git service", http.StatusForbidden)
			return true
		}

		g.infoRefs(w, r, mirror)
		return true
	}

	/*
		POST /owner/repo/git-upload-pack
	*/
	if r.Method == http.MethodPost &&
		strings.HasSuffix(path, "/git-upload-pack") {

		base := strings.TrimSuffix(path, "/git-upload-pack")
		mirror, ok := g.repos[base]
		if !ok {
			return false
		}

		g.uploadPack(w, r, mirror)
		return true
	}

	return false
}

func (g *gitHTTP) command(r *http.Request, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(r.Context(), g.gitCommand, args...)

	cmd.Env = append(
		os.Environ(),

		// upload-pack is read-only. Don't let Git perform optional
		// repository maintenance or locking as a side effect.
		"GIT_OPTIONAL_LOCKS=0",
	)

	// Git HTTP protocol negotiation, particularly protocol v2.
	if protocol := r.Header.Get("Git-Protocol"); protocol != "" {
		cmd.Env = append(cmd.Env, "GIT_PROTOCOL="+protocol)
	}

	return cmd
}

func (g *gitHTTP) infoRefs(
	w http.ResponseWriter,
	r *http.Request,
	mirror string,
) {
	cmd := g.command(
		r,
		"upload-pack",
		"--strict",
		"--http-backend-info-refs",
		mirror,
	)

	var stderr bytes.Buffer

	cmd.Stdout = w
	cmd.Stderr = &stderr

	w.Header().Set(
		"Content-Type",
		"application/x-git-upload-pack-advertisement",
	)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")

	if err := cmd.Run(); err != nil {
		g.logger.Error(
			"git upload-pack advertisement failed",
			"repo", mirror,
			"error", err,
			"stderr", strings.TrimSpace(stderr.String()),
		)
	}
}

func (g *gitHTTP) uploadPack(
	w http.ResponseWriter,
	r *http.Request,
	mirror string,
) {
	cmd := g.command(
		r,
		"upload-pack",
		"--strict",
		"--stateless-rpc",
		mirror,
	)

	var stderr bytes.Buffer

	cmd.Stdin = r.Body
	cmd.Stdout = w
	cmd.Stderr = &stderr

	w.Header().Set(
		"Content-Type",
		"application/x-git-upload-pack-result",
	)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")

	if err := cmd.Run(); err != nil {
		g.logger.Error(
			"git upload-pack failed",
			"repo", mirror,
			"error", err,
			"stderr", strings.TrimSpace(stderr.String()),
		)
	}
}