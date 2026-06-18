package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/tgckpg/flatgit/internal/buildinfo"
	"github.com/tgckpg/flatgit/internal/config"
	"github.com/tgckpg/flatgit/internal/gitcmd"
	"github.com/tgckpg/flatgit/internal/jobqueue"
	"github.com/tgckpg/flatgit/internal/render"
	"github.com/tgckpg/flatgit/internal/server"
	"github.com/tgckpg/flatgit/internal/webhook"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{}))
	if err := run(log, os.Args); err != nil {
		log.Error("flatgit failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, args []string) error {
	if len(args) < 2 {
		usage(args[0])
		return errors.New("missing command")
	}

	switch args[1] {
	case "render":
		return renderCmd(log, args[2:])
	case "serve":
		return serveCmd(log, args[2:])
	case "daemon":
		return daemonCmd(log, args[2:])
	case "version", "-v", "--version":
		fmt.Printf("%s (%s)\n", buildinfo.Version, buildinfo.Timestamp)
		return nil
	case "help", "-h", "--help":
		usage(args[0])
		return nil
	default:
		usage(args[0])
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func renderCmd(log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	cfgPath := fs.String("c", "flatgit.json", "config file")
	repoName := fs.String("repo", "", "repo name to render; empty renders all repos")
	fetch := fs.Bool("fetch", true, "clone/fetch before rendering")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	ctx := context.Background()
	return renderConfigured(ctx, log, cfg, *repoName, *fetch)
}

func serveCmd(log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("c", "", "config file; optional if -root is set")
	addr := fs.String("addr", ":8080", "listen address")
	root := fs.String("root", "", "static root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cfgPath != "" {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			return err
		}
		if *root == "" {
			*root = cfg.WebRoot()
		}
		if *addr == ":8080" && cfg.Addr != "" {
			*addr = cfg.Addr
		}
	}
	if *root == "" {
		return errors.New("serve needs -root or -c")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.ListenAndServe(ctx, server.Options{Addr: *addr, Root: *root, Logger: log})
}

func daemonCmd(log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	cfgPath := fs.String("c", "flatgit.json", "config file")
	renderOnStart := fs.Bool("render-on-start", true, "render all repos on startup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	git := gitcmd.New(cfg.Git.Command)
	r := render.New(git, cfg.PublicURL, cfg.Render.MaxCommits)
	q := jobqueue.New(1024, log)
	q.Start(ctx, cfg.Render.Workers, func(ctx context.Context, name string) error {
		repo, ok := cfg.RepoByName(name)
		if !ok {
			return fmt.Errorf("repo not found: %s", name)
		}
		if err := git.EnsureMirror(ctx, *repo, cfg.CloneTimeout(), cfg.FetchTimeout()); err != nil {
			return err
		}
		return r.RenderRepo(ctx, *repo)
	})

	if *renderOnStart {
		for _, repo := range cfg.Repos {
			q.Enqueue(repo.Name)
		}
	}

	wh := &webhook.Handler{Config: cfg, Queue: q, Logger: log}
	return server.ListenAndServe(ctx, server.Options{
		Addr:   cfg.Addr,
		Root:   cfg.WebRoot(),
		Logger: log,
		WebhookMux: func(mux *http.ServeMux) {
			wh.Register(mux)
		},
	})
}

func repoMatches(repo config.Repo, name string) bool {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "/")

	if name == "" {
		return true
	}

	fullName := strings.Trim(repo.FullName(), "/")
	repoBase := strings.Trim(repo.RepoBase(), "/")

	return strings.EqualFold(repo.Name, name) ||
		strings.EqualFold(fullName, name) ||
		strings.EqualFold(repoBase, name)
}

func renderConfigured(ctx context.Context, log *slog.Logger, cfg *config.Config, repoName string, fetch bool) error {
	git := gitcmd.New(cfg.Git.Command)
	r := render.New(git, cfg.PublicURL, cfg.Render.MaxCommits)

	matched := false

	for i := range cfg.Repos {
		repo := cfg.Repos[i]

		if repoName != "" && !repoMatches(repo, repoName) {
			continue
		}

		matched = true

		log.Info("rendering repo", "repo", repo.FullName(), "base", repo.RepoBase())

		if fetch {
			if err := git.EnsureMirror(ctx, repo, cfg.CloneTimeout(), cfg.FetchTimeout()); err != nil {
				return err
			}
		}

		if err := r.RenderRepo(ctx, repo); err != nil {
			return err
		}

		log.Info("rendered repo", "repo", repo.FullName(), "base", repo.RepoBase(), "output", repo.OutputDir)
	}

	if repoName != "" && !matched {
		return fmt.Errorf("repo not configured: %s", repoName)
	}

	return nil
}

func usage(name string) {
	fmt.Fprintf(os.Stderr, `flatgit - static Git renderer

Usage:
  %[1]s render [-c flatgit.json] [-repo name] [-fetch=true]
  %[1]s serve  [-c flatgit.json] [-root /var/lib/flatgit/www] [-addr :8080]
  %[1]s daemon [-c flatgit.json] [-render-on-start=true]
  %[1]s version

`, name)
}
