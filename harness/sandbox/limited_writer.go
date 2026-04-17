package sandbox

import (
	"io"
	"sync"
)

// limitedWriter wraps an io.Writer and silently discards bytes beyond max.
// It never returns an error for overflow — the subprocess continues running
// but output is truncated.
type limitedWriter struct {
	inner     io.Writer
	max       int64
	written   int64
	truncated bool
	mu        sync.Mutex
}

func newLimitedWriter(inner io.Writer, max int64) *limitedWriter {
	return &limitedWriter{inner: inner, max: max}
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.written >= w.max {
		w.truncated = true
		return len(p), nil // silently discard
	}

	remaining := w.max - w.written
	toWrite := p
	if int64(len(p)) > remaining {
		toWrite = p[:remaining]
		w.truncated = true
	}

	n, err := w.inner.Write(toWrite)
	w.written += int64(n)
	if err != nil {
		return n, err
	}
	// Report all bytes as written even if some were discarded,
	// so the subprocess doesn't get a short write error.
	return len(p), nil
}

func (w *limitedWriter) Truncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.truncated
}

func (w *limitedWriter) Written() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written
}
