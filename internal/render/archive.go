package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tgckpg/flatgit/internal/config"
)

func (r *Renderer) renderArchive(
	ctx context.Context,
	repo config.Repo,
	next string,
	commit string,
	refSlug string,
) error {
	absNext, err := filepath.Abs(next)
	if err != nil {
		return fmt.Errorf("abs next path %s: %w", next, err)
	}

	archiveDir := filepath.Join(absNext, "archive")
	outPath := filepath.Join(archiveDir, refSlug+".zip")

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("mkdir archive dir %s: %w", archiveDir, err)
	}

	prefix := repo.Name + "-" + refSlug + "/"

	_, err = r.Git.Output(
		ctx,
		repo.MirrorDir,
		"archive",
		"--format=zip",
		"--prefix="+prefix,
		"-o",
		outPath,
		commit,
	)
	if err != nil {
		return fmt.Errorf("write archive %s for %s at %s: %w", outPath, repo.FullName(), commit, err)
	}

	return nil
}
