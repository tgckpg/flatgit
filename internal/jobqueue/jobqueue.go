package jobqueue

import (
	"context"
	"log/slog"
	"sync"
)

type Handler func(context.Context, string) error

type Queue struct {
	ch      chan string
	mu      sync.Mutex
	queued  map[string]bool
	running map[string]bool
	log     *slog.Logger
}

func New(size int, log *slog.Logger) *Queue {
	if size <= 0 {
		size = 128
	}
	if log == nil {
		log = slog.Default()
	}
	return &Queue{
		ch:      make(chan string, size),
		queued:  make(map[string]bool),
		running: make(map[string]bool),
		log:     log,
	}
}

func (q *Queue) Enqueue(name string) bool {
	q.mu.Lock()
	if q.queued[name] || q.running[name] {
		q.mu.Unlock()
		return false
	}
	q.queued[name] = true
	q.mu.Unlock()

	select {
	case q.ch <- name:
		return true
	default:
		q.mu.Lock()
		delete(q.queued, name)
		q.mu.Unlock()
		return false
	}
}

func (q *Queue) Start(ctx context.Context, workers int, h Handler) {
	if workers <= 0 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		go q.worker(ctx, i, h)
	}
}

func (q *Queue) worker(ctx context.Context, id int, h Handler) {
	for {
		select {
		case <-ctx.Done():
			return
		case name, ok := <-q.ch:
			if !ok {
				return
			}
			q.mu.Lock()
			delete(q.queued, name)
			q.running[name] = true
			q.mu.Unlock()

			q.log.Info("render job started", "worker", id, "repo", name)
			if err := h(ctx, name); err != nil {
				q.log.Error("render job failed", "worker", id, "repo", name, "err", err)
			} else {
				q.log.Info("render job finished", "worker", id, "repo", name)
			}

			q.mu.Lock()
			delete(q.running, name)
			q.mu.Unlock()
		}
	}
}
