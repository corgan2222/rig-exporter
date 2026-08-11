//go:build windows

package pawnio

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/corgan2222/rig-exporter/internal/applog"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// blockingLoader holds the load until the test releases it, which is what the
// real download does for up to two minutes when the module is not on disk yet.
type blockingLoader struct {
	release chan struct{}
	calls   atomic.Int32
	err     error
}

func (l *blockingLoader) Load(ctx context.Context, _ string) ([]byte, error) {
	l.calls.Add(1)
	select {
	case <-l.release:
		return []byte{0x01}, l.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// The tick drives every other source with it. It must not wait on the module
// download: two minutes without a snapshot is two minutes of "unavailable" in
// Home Assistant, for CPU, RAM, GPU and FPS alike.
func TestTheFirstCollectDoesNotWaitForTheDownload(t *testing.T) {
	loader := &blockingLoader{release: make(chan struct{})}
	t.Cleanup(func() { close(loader.release) })

	s := newSourceWithLoader(loader, applog.Discard())
	s.model = "AMD Ryzen 9 5950X" // otherwise the Intel branch answers first
	t.Cleanup(s.Close)

	var set metrics.Set
	done := make(chan struct{})
	go func() { defer close(done); _ = s.Collect(&set) }()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Collect blocked on the module download")
	}
}

// A captive portal is not a permanent verdict. once.Do used to make one out of
// it: the laptop came back onto the network and this source stayed silent until
// the program was restarted.
func TestATransientDownloadFailureIsRetried(t *testing.T) {
	loader := &blockingLoader{release: make(chan struct{}), err: errors.New("no route to host")}
	close(loader.release)

	s := newSourceWithLoader(loader, applog.Discard())
	s.model = "AMD Ryzen 9 5950X"
	s.backoff = time.Millisecond // so the test does not sit out the real one
	t.Cleanup(s.Close)

	var set metrics.Set
	waitFor(t, time.Second, func() bool {
		_ = s.Collect(&set)
		return loader.calls.Load() >= 2
	})
}

// "Only AMD is supported" cannot change while the program runs, so it is the
// one answer worth remembering. Retrying it every minute would be noise.
func TestAnUnsupportedProcessorIsNotRetried(t *testing.T) {
	loader := &blockingLoader{release: make(chan struct{})}
	close(loader.release)

	s := newSourceWithLoader(loader, applog.Discard())
	s.model = "Intel Core i9-13900K"
	s.backoff = time.Millisecond
	t.Cleanup(s.Close)

	var set metrics.Set
	for range 20 {
		_ = s.Collect(&set)
		time.Sleep(time.Millisecond)
	}
	if calls := loader.calls.Load(); calls != 0 {
		t.Errorf("an unsupported processor reached the loader %d times", calls)
	}
}

// "Quit" from the tray must not wait out a download. Stop() closes the loop
// first and releases the sources afterwards, so the load has to answer to the
// release rather than to its own two-minute deadline. A configuration change
// takes the same path: switching PawnIO off rebuilds the whole set.
func TestClosingCancelsAPendingDownload(t *testing.T) {
	loader := &blockingLoader{release: make(chan struct{})}
	t.Cleanup(func() { close(loader.release) })

	s := newSourceWithLoader(loader, applog.Discard())
	s.model = "AMD Ryzen 9 5950X"

	var set metrics.Set
	_ = s.Collect(&set) // kicks the load off
	waitFor(t, time.Second, func() bool { return loader.calls.Load() == 1 })

	s.Close()

	waitFor(t, time.Second, func() bool {
		return errors.Is(s.Collect(&set), context.Canceled)
	})
}

// waitFor polls until the condition holds, failing the test if it never does.
func waitFor(t *testing.T, limit time.Duration, ok func() bool) {
	t.Helper()

	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the condition never held")
}
