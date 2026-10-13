package logging

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countingExec struct {
	n     atomic.Int64
	delay time.Duration
}

func (e *countingExec) Exec(map[string]string, []byte) error {
	time.Sleep(e.delay)
	e.n.Add(1)
	return nil
}
func (e *countingExec) Close() error { return nil }

// Writes racing with Flush used to send on the closed queue and panic.
func TestHookWriteDuringFlush(t *testing.T) {
	for i := 0; i < 50; i++ {
		h := NewHook(&countingExec{}, SetHookMaxJobs(8), SetHookMaxWorkers(2))
		var wg sync.WaitGroup
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					_, _ = h.Write([]byte(`{"msg":"x"}`))
				}
			}()
		}
		h.Flush()
		wg.Wait()
		h.Flush() // idempotent
	}
}

func TestHookWriteNeverBlocks(t *testing.T) {
	exec := &countingExec{delay: time.Second}
	h := NewHook(exec, SetHookMaxJobs(1), SetHookMaxWorkers(1))
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_, _ = h.Write([]byte("x"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Write blocked on a full queue")
	}
}

func TestHookFlushTimeout(t *testing.T) {
	orig := hookFlushTimeout
	hookFlushTimeout = 100 * time.Millisecond
	t.Cleanup(func() { hookFlushTimeout = orig })

	h := NewHook(&countingExec{delay: 5 * time.Second}, SetHookMaxJobs(4), SetHookMaxWorkers(1))
	_, _ = h.Write([]byte("x"))
	_, _ = h.Write([]byte("x"))
	start := time.Now()
	h.Flush()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Flush waited %v despite the timeout", d)
	}
}
