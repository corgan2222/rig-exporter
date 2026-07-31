// Package applog sets up file logging.
//
// rig-exporter is built with -H windowsgui and therefore has no console to write
// to, so the log file is the only place errors can be seen. It rotates by size
// to stay bounded without a dependency.
package applog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// maxBytes is the size at which the log is renamed to <name>.1. One backup is
// kept, so disk usage stays under roughly twice this.
const maxBytes = 2 << 20

// Setup opens path for appending and returns a logger writing to it. The
// returned closer flushes and releases the file.
func Setup(path string, debug bool) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create log dir: %w", err)
	}

	w, err := newRotator(path)
	if err != nil {
		return nil, nil, err
	}

	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
	return logger, w, nil
}

// Discard returns a logger that throws everything away, for tests and for the
// window before the log file is open.
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// rotator is an io.WriteCloser that renames the log aside once it grows past
// maxBytes.
type rotator struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func newRotator(path string) (*rotator, error) {
	r := &rotator{path: path}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotator) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", r.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log %s: %w", r.path, err)
	}
	r.file = f
	r.size = info.Size()
	return nil
}

func (r *rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size+int64(len(p)) > maxBytes {
		r.rotate()
	}
	if r.file == nil {
		return 0, fmt.Errorf("log %s is not open", r.path)
	}

	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate renames the current log aside and reopens an empty one. Any failure
// along the way ends with the old file reopened, because losing log lines is
// worse than a log that overshoots its size limit.
func (r *rotator) rotate() {
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}

	backup := r.path + ".1"
	os.Remove(backup)
	os.Rename(r.path, backup) // best effort: an antivirus lock leaves it in place

	if err := r.open(); err != nil {
		r.file = nil
	}
}

func (r *rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
