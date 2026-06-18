package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
)

type Options struct {
	Addr       string
	Root       string
	Logger     *slog.Logger
	WebhookMux func(*http.ServeMux)
}

func withCacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ext := path.Ext(r.URL.Path)

		switch ext {
		case ".html", ".json", ".xml", ".txt":
			w.Header().Set("Cache-Control", "no-cache")
		case ".css", ".js", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2":
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			// Raw files are tricky.
			// For now, keep them revalidating unless we make commit-addressed raw URLs.
			w.Header().Set("Cache-Control", "no-cache")
		}

		next.ServeHTTP(w, r)
	})
}

func ListenAndServe(ctx context.Context, opts Options) error {
	if opts.Addr == "" {
		opts.Addr = ":8080"
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	if opts.WebhookMux != nil {
		opts.WebhookMux(mux)
	}
	fs := http.FileServer(http.Dir(opts.Root))
	mux.Handle("/", withCacheHeaders(fs))

	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           logRequests(opts.Logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	opts.Logger.Info("flatgit serving", "addr", opts.Addr, "root", opts.Root)

	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		opts.Logger.Info("flatgit shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	}
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/healthz") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Info("http request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "dur", time.Since(start))
	})
}
