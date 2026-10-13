package logging

import (
	"fmt"
	"sync"
	"time"
)

type HookExecuter interface {
	Exec(extra map[string]string, b []byte) error
	Close() error
}

type hookOptions struct {
	maxJobs    int
	maxWorkers int
	extra      map[string]string
}

// Set the number of buffers
func SetHookMaxJobs(maxJobs int) HookOption {
	return func(o *hookOptions) {
		o.maxJobs = maxJobs
	}
}

// Set the number of worker threads
func SetHookMaxWorkers(maxWorkers int) HookOption {
	return func(o *hookOptions) {
		o.maxWorkers = maxWorkers
	}
}

// Set extended parameters
func SetHookExtra(extra map[string]string) HookOption {
	return func(o *hookOptions) {
		o.extra = extra
	}
}

// HookOption a hook parameter options
type HookOption func(*hookOptions)

// Creates a hook to be added to an instance of logger
func NewHook(exec HookExecuter, opt ...HookOption) *Hook {
	opts := &hookOptions{
		maxJobs:    1024,
		maxWorkers: 2,
	}

	for _, o := range opt {
		o(opts)
	}

	wg := new(sync.WaitGroup)
	wg.Add(opts.maxWorkers)

	h := &Hook{
		opts: opts,
		q:    make(chan []byte, opts.maxJobs),
		wg:   wg,
		e:    exec,
	}
	h.dispatch()
	return h
}

// Hook writes log entries to an executor (for example a database) from a
// bounded queue served by background workers.
type Hook struct {
	opts   *hookOptions
	q      chan []byte
	wg     *sync.WaitGroup
	e      HookExecuter
	mu     sync.RWMutex // guards closed against close(q)
	closed bool
}

// hookFlushTimeout bounds how long Flush waits for queued entries, so a
// database outage cannot stall shutdown.
var hookFlushTimeout = 10 * time.Second

func (h *Hook) dispatch() {
	for i := 0; i < h.opts.maxWorkers; i++ {
		go func() {
			defer func() {
				h.wg.Done()
				if r := recover(); r != nil {
					fmt.Println("Recovered from panic in logger hook:", r)
				}
			}()

			for data := range h.q {
				err := h.e.Exec(h.opts.extra, data)
				if err != nil {
					fmt.Println("Failed to write entry:", err.Error())
				}
			}
		}()
	}
}

// Write queues a copy of p. It never blocks: when the queue is full or the
// hook is closed, the entry is dropped.
func (h *Hook) Write(p []byte) (int, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return len(p), nil
	}

	data := make([]byte, len(p))
	copy(data, p)
	select {
	case h.q <- data:
	default:
		fmt.Println("Logger hook queue is full, entry discarded")
	}
	return len(p), nil
}

// Flush stops accepting entries and waits, up to hookFlushTimeout, for the
// queued ones to be written.
func (h *Hook) Flush() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.q)
	h.mu.Unlock()

	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(hookFlushTimeout):
		fmt.Println("Logger hook flush timed out, remaining entries discarded")
		return
	}
	if err := h.e.Close(); err != nil {
		fmt.Println("Failed to close logger hook:", err.Error())
	}
}
