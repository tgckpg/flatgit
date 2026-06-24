package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tgckpg/flatgit/internal/config"
)

type Enqueuer interface {
	Enqueue(string) bool
}

type Handler struct {
	Config  *config.Config
	Queue   Enqueuer
	Logger  *slog.Logger
	MaxBody int64
}

type payload struct {
	Ref        string `json:"ref"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/webhook/gitea", h.ServeHTTP)
	mux.HandleFunc("/webhook/github", h.ServeHTTP)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := h.Logger

	if log != nil {

		xff := r.Header.Get("X-Forwarded-For")
		xrealip := r.Header.Get("X-Real-IP")

		log.Info("webhook request",
			"method", r.Method,
			"path", r.URL.Path,
			"x_forwarded_for", xff,
			"x_real_ip", xrealip,
			"remote", r.RemoteAddr,
			"event", r.Header.Get("X-Gitea-Event"),
			"content_type", r.Header.Get("Content-Type"),
			"delivery", r.Header.Get("X-Gitea-Delivery"),
			"signature", r.Header.Get("X-Gitea-Signature") != "",
			"signature_256", r.Header.Get("X-Gitea-Signature-256") != "",
		)
	}

	if r.Method != http.MethodPost {
		if log != nil {
			log.Warn("webhook rejected", "reason", "method not allowed", "method", r.Method)
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.MaxBody <= 0 {
		h.MaxBody = 1 << 20
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.MaxBody))
	if err != nil {
		if log != nil {
			log.Warn("webhook rejected", "reason", "bad body", "error", err)
		}
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}

	if log != nil {
		log.Info("webhook body read", "bytes", len(body))
	}

	if !verifySignature(h.Config.Webhook.Secret, body, r) {
		if log != nil {
			log.Warn("webhook rejected",
				"reason", "bad signature",
				"secret_configured", h.Config.Webhook.Secret != "",
				"signature", r.Header.Get("X-Gitea-Signature"),
				"signature_256", r.Header.Get("X-Gitea-Signature-256"),
			)
		}
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}

	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		if log != nil {
			log.Warn("webhook rejected", "reason", "bad json", "error", err)
		}
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	fullName := firstNonEmpty(p.Repository.FullName, p.Repository.Name)

	if log != nil {
		log.Info("webhook payload",
			"ref", p.Ref,
			"repo_name", p.Repository.Name,
			"repo_full_name", p.Repository.FullName,
			"selected_name", fullName,
		)
	}

	if fullName == "" {
		if log != nil {
			log.Warn("webhook rejected", "reason", "missing repository name")
		}
		http.Error(w, "missing repository name", http.StatusBadRequest)
		return
	}

	repo, ok := h.Config.RepoByWebhookName(fullName)
	if !ok {
		if log != nil {
			var configured []string
			for _, r := range h.Config.Repos {
				configured = append(configured, r.FullName())
			}

			log.Warn("webhook rejected",
				"reason", "repo not configured",
				"payload_repo", fullName,
				"configured_repos", configured,
			)
		}

		http.Error(w, fmt.Sprintf("repo not configured: %s", fullName), http.StatusNotFound)
		return
	}

	queued := h.Queue.Enqueue(repo.FullName())

	if log != nil {
		h.Logger.Info("webhook accepted",
			"payload_repo", fullName,
			"repo", repo.FullName(),
			"queue_name", repo.Name,
			"ref", p.Ref,
			"queued", queued,
		)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, `{"ok":true,"queued":%t,"repo":%q}`+"\n", queued, repo.FullName())
}

func verifySignature(secret string, body []byte, r *http.Request) bool {
	if secret == "" {
		return true
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)

	github := r.Header.Get("X-Hub-Signature-256")
	if strings.HasPrefix(github, "sha256=") {
		got, err := hex.DecodeString(strings.TrimPrefix(github, "sha256="))
		return err == nil && hmac.Equal(got, expected)
	}

	gitea := r.Header.Get("X-Gitea-Signature")
	if gitea != "" {
		got, err := hex.DecodeString(gitea)
		return err == nil && hmac.Equal(got, expected)
	}

	return false
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
