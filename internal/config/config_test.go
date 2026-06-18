package config

import "testing"

func TestDefaults(t *testing.T) {
	cfg := &Config{Repos: []Repo{{Name: "test-repo", Owner: "penguin"}}}
	if err := cfg.ApplyDefaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("addr = %q", cfg.Addr)
	}
	if cfg.Repos[0].RepoBase() != "/penguin/test-repo/" {
		t.Fatalf("slug = %q", cfg.Repos[0].RepoBase())
	}
	if cfg.Repos[0].MirrorDir == "" || cfg.Repos[0].OutputDir == "" {
		t.Fatalf("paths were not defaulted")
	}
}
