package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Addr      string        `json:"addr"`
	DataDir   string        `json:"data_dir"`
	PublicURL string        `json:"public_url"`
	Webhook   WebhookConfig `json:"webhook"`
	Git       GitConfig     `json:"git"`
	Render    RenderConfig  `json:"render"`
	Repos     []Repo        `json:"repos"`
}

type WebhookConfig struct {
	Secret string `json:"secret"`
}

type GitConfig struct {
	Command      string `json:"command"`
	CloneTimeout string `json:"clone_timeout"`
	FetchTimeout string `json:"fetch_timeout"`
}

type RenderConfig struct {
	Workers    int `json:"workers"`
	MaxCommits int `json:"max_commits"`
}

type Repo struct {
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	URL           string `json:"url"`
	DefaultBranch string `json:"default_branch"`
	MirrorDir     string `json:"mirror_dir"`
	OutputDir     string `json:"output_dir"`
	Description   string `json:"description"`

	fullName string
	repoBase string
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.ApplyDefaults(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) ApplyDefaults() error {
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	if c.DataDir == "" {
		c.DataDir = "/var/lib/flatgit"
	}
	if c.Git.Command == "" {
		c.Git.Command = "git"
	}
	if c.Git.CloneTimeout == "" {
		c.Git.CloneTimeout = "2m"
	}
	if c.Git.FetchTimeout == "" {
		c.Git.FetchTimeout = "2m"
	}
	if c.Render.Workers <= 0 {
		c.Render.Workers = 1
	}
	if c.Render.MaxCommits <= 0 {
		c.Render.MaxCommits = 500
	}

	seen := make(map[string]bool, len(c.Repos))
	for i := range c.Repos {
		r := &c.Repos[i]
		if r.Name == "" {
			return errors.New("repo missing name")
		}
		if seen[r.Name] {
			return fmt.Errorf("duplicate repo name %q", r.Name)
		}
		seen[r.Name] = true

		r.Name = cleanName(r.Name)
		if r.Owner != "" {
			r.Owner = cleanName(r.Owner)
		}
		if r.DefaultBranch == "" {
			r.DefaultBranch = "main"
		}
		if r.MirrorDir == "" {
			r.MirrorDir = filepath.Join(c.DataDir, "repos", r.RepoBase()+".git")
		}
		if r.OutputDir == "" {
			r.OutputDir = filepath.Join(c.DataDir, "www", r.RepoBase())
		}
	}
	return nil
}

func (c *Config) CloneTimeout() time.Duration {
	return parseDuration(c.Git.CloneTimeout, 2*time.Minute)
}

func (c *Config) FetchTimeout() time.Duration {
	return parseDuration(c.Git.FetchTimeout, 2*time.Minute)
}

func (c *Config) WebRoot() string {
	return filepath.Join(c.DataDir, "www")
}

func (c Config) RepoByName(name string) (*Repo, bool) {
	for _, repo := range c.Repos {
		if repo.FullName() == name || repo.Name == name {
			return &repo, true
		}
	}
	return &Repo{}, false
}

func (c Config) RepoByWebhookName(name string) (Repo, bool) {
	for _, repo := range c.Repos {
		if repo.FullName() == name {
			return repo, true
		}

		// Fallback for single-owner or old/simple configs.
		if repo.Name == name {
			return repo, true
		}
	}

	return Repo{}, false
}

func (r Repo) FullName() string {
	if r.fullName != "" {
		return r.fullName
	}

	if r.Owner == "" {
		return r.Name
	}

	return r.Owner + "/" + r.Name
}

func (r Repo) RepoBase() string {
	if r.repoBase != "" {
		return r.repoBase
	}

	if r.Owner == "" {
		return "/" + url.PathEscape(r.Name) + "/"
	}

	return "/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name) + "/"
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "..", "_")
	return s
}
