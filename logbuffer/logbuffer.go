package logbuffer

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const maxEntries = 1000

// Entry is a single buffered log line.
type Entry struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

var (
	mu      sync.RWMutex
	buf     = make([]Entry, 0, maxEntries)
	writeAt int // next write position (ring index)
	full    bool
)

// add inserts an entry into the ring buffer (caller must hold mu write lock).
func add(e Entry) {
	if len(buf) < maxEntries {
		buf = append(buf, e)
	} else {
		buf[writeAt] = e
		writeAt = (writeAt + 1) % maxEntries
		full = true
	}
}

// GetRecent returns the last n entries in chronological order.
func GetRecent(n int) []Entry {
	mu.RLock()
	defer mu.RUnlock()
	total := len(buf)
	if n > total {
		n = total
	}
	if n == 0 {
		return []Entry{}
	}
	result := make([]Entry, n)
	if !full {
		// buf is not yet wrapped; newest entries are at the end
		copy(result, buf[total-n:])
		return result
	}
	// ring is full; writeAt points to the oldest entry
	// Build ordered slice: oldest→newest starting from writeAt
	ordered := make([]Entry, maxEntries)
	copy(ordered[:maxEntries-writeAt], buf[writeAt:])
	copy(ordered[maxEntries-writeAt:], buf[:writeAt])
	copy(result, ordered[maxEntries-n:])
	return result
}

// bufHandler wraps an inner slog.Handler and captures each record into the ring buffer.
type bufHandler struct {
	inner slog.Handler
}

// NewHandler wraps inner and additionally writes every record to the ring buffer.
func NewHandler(inner slog.Handler) slog.Handler {
	return &bufHandler{inner: inner}
}

func (h *bufHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *bufHandler) Handle(ctx context.Context, r slog.Record) error {
	attrs := make(map[string]string)
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	if len(attrs) == 0 {
		attrs = nil
	}
	e := Entry{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
		Attrs:   attrs,
	}
	mu.Lock()
	add(e)
	mu.Unlock()
	return h.inner.Handle(ctx, r)
}

func (h *bufHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &bufHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *bufHandler) WithGroup(name string) slog.Handler {
	return &bufHandler{inner: h.inner.WithGroup(name)}
}
