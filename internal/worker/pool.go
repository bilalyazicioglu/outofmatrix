// Package worker implements a bounded, channel-based worker pool for
// background media processing. The worker count is configurable so heavy
// FFmpeg jobs cannot overwhelm a low-spec home server.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ErrQueueFull is returned by Enqueue when the job buffer is at capacity.
// Callers decide the policy (here: uploads stay "pending" in the database and
// are recovered at the next boot).
var ErrQueueFull = errors.New("worker: job queue is full")

// Job is one unit of background work: process the media item with this ID.
type Job struct {
	MediaID uuid.UUID
}

// Handler processes a single job. Errors are logged by the pool; retry and
// status bookkeeping are the handler's responsibility.
type Handler func(ctx context.Context, job Job) error

// Pool is a fixed-size worker pool fed by a buffered channel.
type Pool struct {
	size       int
	jobTimeout time.Duration
	jobs       chan Job
	log        *slog.Logger

	started atomic.Bool
	wg      sync.WaitGroup
}

// New creates a pool with `size` workers and a queue buffer of `queueCap`
// jobs. jobTimeout bounds each job (0 = no per-job timeout).
func New(size, queueCap int, jobTimeout time.Duration, log *slog.Logger) *Pool {
	if size < 1 {
		size = 1
	}
	if queueCap < 1 {
		queueCap = 1
	}
	if log == nil {
		log = slog.Default()
	}
	return &Pool{
		size:       size,
		jobTimeout: jobTimeout,
		jobs:       make(chan Job, queueCap),
		log:        log,
	}
}

// Start launches the workers. Each worker pulls jobs until Shutdown closes
// the queue or ctx is cancelled; cancelling ctx also cancels in-flight jobs,
// which kills any running FFmpeg subprocess via exec.CommandContext.
func (p *Pool) Start(ctx context.Context, handle Handler) {
	if !p.started.CompareAndSwap(false, true) {
		p.log.Warn("worker pool already started")
		return
	}
	for i := 0; i < p.size; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i, handle)
	}
	p.log.Info("worker pool started", "workers", p.size, "queue_capacity", cap(p.jobs))
}

func (p *Pool) worker(ctx context.Context, id int, handle Handler) {
	defer p.wg.Done()
	log := p.log.With("worker", id)
	for {
		select {
		case <-ctx.Done():
			log.Info("worker stopping: context cancelled")
			return
		case job, ok := <-p.jobs:
			if !ok {
				log.Info("worker stopping: queue closed")
				return
			}
			p.runJob(ctx, log, handle, job)
		}
	}
}

func (p *Pool) runJob(ctx context.Context, log *slog.Logger, handle Handler, job Job) {
	// A panicking job must never take its worker goroutine (and eventually
	// the whole pool) down with it.
	defer func() {
		if r := recover(); r != nil {
			log.Error("job panicked", "media_id", job.MediaID, "panic", r)
		}
	}()

	jobCtx := ctx
	if p.jobTimeout > 0 {
		var cancel context.CancelFunc
		jobCtx, cancel = context.WithTimeout(ctx, p.jobTimeout)
		defer cancel()
	}

	start := time.Now()
	log.Info("job started", "media_id", job.MediaID)
	if err := handle(jobCtx, job); err != nil {
		log.Error("job failed", "media_id", job.MediaID, "duration", time.Since(start).String(), "error", err)
		return
	}
	log.Info("job finished", "media_id", job.MediaID, "duration", time.Since(start).String())
}

// Enqueue adds a job without blocking. It returns ErrQueueFull when the
// buffer is saturated, which lets HTTP handlers respond immediately instead
// of stalling uploads behind a busy pipeline.
func (p *Pool) Enqueue(job Job) error {
	select {
	case p.jobs <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

// QueueDepth reports how many jobs are waiting (useful for health output).
func (p *Pool) QueueDepth() int {
	return len(p.jobs)
}

// Shutdown stops accepting work implicitly (callers should stop enqueueing
// first), drains the queue, and waits for in-flight jobs to finish. Use a
// cancelled Start context instead when a hard stop is needed.
func (p *Pool) Shutdown() {
	close(p.jobs)
	p.wg.Wait()
	p.log.Info("worker pool drained and stopped")
}
