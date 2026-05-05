package vastai

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

// embeddingRunning is set to 1 while RunEmbedding holds a GPU.
// The embedding worker's idle-GPU shutdown checks this to avoid
// destroying an instance that is actively being used.
var embeddingRunning atomic.Int32

// IsEmbeddingRunning reports whether RunEmbedding is currently holding a GPU.
func IsEmbeddingRunning() bool { return embeddingRunning.Load() > 0 }

// RunEmbedding acquires a GPU, sets EMBEDDING_URL to point to it, calls fn,
// then destroys the instance (if it was newly created).
// If VASTAI_API_KEY is not set or no GPU is needed, fn is called with the
// current EMBEDDING_URL unchanged and no instance is managed.
func RunEmbedding(model string, fn func() error) error {
	if apiKey() == "" {
		return fn()
	}

	embeddingRunning.Add(1)
	defer embeddingRunning.Add(-1)

	instanceID, ollamaURL, isNew, err := AcquireEmbedGPU(model)
	if err != nil {
		return fmt.Errorf("vastai: acquire GPU: %w", err)
	}

	// Point embedding worker at the new instance
	original := os.Getenv("EMBEDDING_URL")
	os.Setenv("EMBEDDING_URL", ollamaURL)
	slog.Info("vastai: embedding URL set", "url", ollamaURL, "instance_id", instanceID)

	// Run the embedding workload
	fnErr := fn()

	// Restore original URL
	os.Setenv("EMBEDDING_URL", original)

	// Always tear down after the embedding job — regardless of whether this
	// instance was freshly rented or reused. Work is done; keep nothing running.
	_ = isNew
	slog.Info("vastai: destroying instance after embedding", "instance_id", instanceID)
	if err := DestroyInstance(instanceID); err != nil {
		slog.Warn("vastai: failed to destroy instance", "instance_id", instanceID, "err", err)
	} else {
		slog.Info("vastai: instance destroyed", "instance_id", instanceID)
	}

	return fnErr
}
