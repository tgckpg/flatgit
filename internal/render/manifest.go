package render

import (
	"path"
	"strings"
	"time"

	"github.com/tgckpg/flatgit/internal/config"
)

type Manifest struct {
	Schema       string               `json:"schema"`
	Generator    ManifestGenerator    `json:"generator"`
	Repository   ManifestRepository   `json:"repository"`
	Human        ManifestHumanRoutes  `json:"human"`
	Machine      ManifestAPIRoutes    `json:"machine"`
	Capabilities ManifestCapabilities `json:"capabilities"`
	GeneratedAt  time.Time            `json:"generated_at"`
}

type ManifestGenerator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ManifestRepository struct {
	Name           string `json:"name"`
	FullName       string `json:"fullname"`
	Owner          string `json:"owner,omitempty"`
	Description    string `json:"description,omitempty"`
	DefaultBranch  string `json:"default_branch"`
	DefaultCommit  string `json:"default_commit"`
	DefaultRefSlug string `json:"default_ref_slug"`
	SitePath       string `json:"site_path"`
	OwnerSitePath  string `json:"owner_site_path"`
}

type ManifestHumanRoutes struct {
	Index  string `json:"index"`
	Log    string `json:"log"`
	Refs   string `json:"refs"`
	Tree   string `json:"tree"`
	Blob   string `json:"blob"`
	Commit string `json:"commit"`
}

type ManifestAPIRoutes struct {
	Self    string  `json:"self"`
	Refs    string  `json:"refs"`
	Commits string  `json:"commits"`
	Tree    string  `json:"tree"`
	Commit  string  `json:"commit"`
	Blob    *string `json:"blob,omitempty"`
	Raw     string  `json:"raw"`
}

type ManifestCapabilities struct {
	Refs         bool `json:"refs"`
	Commits      bool `json:"commits"`
	Trees        bool `json:"trees"`
	BlobMetadata bool `json:"blob_metadata"`
	RawBlobs     bool `json:"raw_blobs"`
	Search       bool `json:"search"`
	Archive      bool `json:"archive"`
}

func NewManifest(repo config.Repo, defaultBranch string, defaultCommit string) Manifest {
	return Manifest{
		Schema: "https://flatgit.dev/schema/repository.v1.json",
		Generator: ManifestGenerator{
			Name:    "flatgit",
			Version: "dev",
		},
		Repository: ManifestRepository{
			Name:           repo.Name,
			FullName:       repo.FullName(),
			Owner:          repo.Owner,
			Description:    repo.Description,
			DefaultBranch:  defaultBranch,
			DefaultCommit:  defaultCommit,
			DefaultRefSlug: refSlug(repo.DefaultBranch),
			SitePath:       repo.RepoBase(),
			OwnerSitePath:  path.Dir(strings.TrimSuffix(repo.RepoBase(), "/")) + "/",
		},
		Human: ManifestHumanRoutes{
			Index:  "./index.html",
			Log:    "./log.html",
			Refs:   "./refs.html",
			Tree:   "./tree/{ref}/",
			Blob:   "./blob/{ref}/{path}.html",
			Commit: "./commit/{sha}.html",
		},
		Machine: ManifestAPIRoutes{
			Self:    "./manifest.json",
			Refs:    "./refs.json",
			Commits: "./commits.json",
			Tree:    "./tree/{ref}/tree.json",
			Commit:  "./commit/{sha}.json",
			Blob:    nil,
			Raw:     "./raw/{ref}/{path}",
		},
		Capabilities: ManifestCapabilities{
			Refs:         true,
			Commits:      true,
			Trees:        true,
			BlobMetadata: false,
			RawBlobs:     true,
			Search:       false,
			Archive:      false,
		},
		GeneratedAt: time.Now(),
	}
}
