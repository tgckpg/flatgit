package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgckpg/flatgit/internal/config"
)

type Runner struct {
	GitCommand string
	Logger     *slog.Logger
}

func New(gitCommand string, logger *slog.Logger) *Runner {
	if gitCommand == "" {
		gitCommand = "git"
	}
	return &Runner{
		GitCommand: gitCommand,
		Logger:     logger,
	}
}

func (r *Runner) waitRemoteLive(ctx context.Context, url string, timeout time.Duration) error {
	const attempts = 5

	backoff := 500 * time.Millisecond

	var lastErr error

	for i := 0; i < attempts; i++ {
		probeCtx, cancel := context.WithTimeout(ctx, timeout)

		_, err := r.Output(probeCtx, "", "ls-remote", "--heads", url)

		cancel()

		if err == nil {
			return nil
		}

		lastErr = err

		if i == attempts-1 {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
	}

	return lastErr
}

func (r *Runner) EnsureMirror(ctx context.Context, repo config.Repo, cloneTimeout, fetchTimeout time.Duration) error {
	if repo.URL == "" {
		return fmt.Errorf("repo %s has no url", repo.FullName())
	}

	if err := r.waitRemoteLive(ctx, repo.URL, fetchTimeout); err != nil {
		return fmt.Errorf("repo %s remote not reachable: %w", repo.FullName(), err)
	}

	lockPath := repo.MirrorDir + ".lock"
	unlock, err := acquireLock(lockPath)
	if err != nil {
		return err
	}
	defer unlock()

	if isGitDir(repo.MirrorDir) {
		ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		defer cancel()

		_, err := r.Output(ctx, repo.MirrorDir, "remote", "update", "--prune")
		if err != nil {
			return err
		}

		_, _ = r.Output(context.Background(), repo.MirrorDir, "pack-refs", "--all", "--prune")
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(repo.MirrorDir), 0o755); err != nil {
		return err
	}

	tmp := repo.MirrorDir + ".tmp"
	_ = os.RemoveAll(tmp)

	ctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()

	if _, err := r.Output(ctx, "", "clone", "--mirror", repo.URL, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}

	_ = os.RemoveAll(repo.MirrorDir)

	if err := os.Rename(tmp, repo.MirrorDir); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}

	return nil
}

func (r *Runner) Output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.GitCommand, args...)
	cmd.Env = os.Environ()

	if dir != "" {
		cmd.Dir = dir
	}

	if r.Logger != nil {
		r.Logger.Debug("git command started",
			"dir", cmd.Dir,
			"cmd", r.GitCommand,
			"args", args,
		)
	}

	start := time.Now()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		dur := time.Since(start)
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}

		if r.Logger != nil {
			r.Logger.Warn("git command failed",
				"dir", cmd.Dir,
				"cmd", r.GitCommand,
				"args", args,
				"dur", dur,
				"stdout_bytes", stdout.Len(),
				"stderr", msg,
			)
		}

		return stdout.Bytes(), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}

	if r.Logger != nil {
		r.Logger.Debug("git command finished",
			"dir", cmd.Dir,
			"cmd", r.GitCommand,
			"args", args,
			"dur", time.Since(start),
			"stdout_bytes", stdout.Len(),
			"stderr_bytes", stderr.Len(),
		)
	}

	return stdout.Bytes(), nil
}

func (r *Runner) Text(ctx context.Context, dir string, args ...string) (string, error) {
	b, err := r.Output(ctx, dir, args...)
	return string(b), err
}

func isGitDir(path string) bool {
	st, err := os.Stat(filepath.Join(path, "objects"))
	if err != nil || !st.IsDir() {
		return false
	}
	st, err = os.Stat(filepath.Join(path, "refs"))
	return err == nil && st.IsDir()
}

func acquireLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("repo lock exists: %s", path)
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Close()
	return func() { _ = os.Remove(path) }, nil
}
