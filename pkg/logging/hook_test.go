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

type batchExec struct {
	mu      sync.Mutex
	batches []int
}

func (e *batchExec) Exec(map[string]string, []byte) error {
	panic("Exec must not be used when batching")
}
func (e *batchExec) ExecBatch(_ map[string]string, entries [][]byte) error {
	time.Sleep(10 * time.Millisecond) // a slow database lets the queue fill
	e.mu.Lock()
	e.batches = append(e.batches, len(entries))
	e.mu.Unlock()
	return nil
}
func (e *batchExec) Close() error { return nil }

func TestHookBatchesQueuedEntries(t *testing.T) {
	exec := &batchExec{}
	h := NewHook(exec, SetHookMaxJobs(1024), SetHookMaxWorkers(1))
	for i := 0; i < 500; i++ {
		_, _ = h.Write([]byte(`{"msg":"x"}`))
	}
	h.Flush()

	total, largest := 0, 0
	for _, n := range exec.batches {
		total += n
		largest = max(largest, n)
	}
	if total != 500 {
		t.Fatalf("wrote %d entries, want 500", total)
	}
	if largest < 2 || largest > maxHookBatch {
		t.Fatalf("batches should group queued entries up to %d, got sizes %v", maxHookBatch, exec.batches)
	}
}
