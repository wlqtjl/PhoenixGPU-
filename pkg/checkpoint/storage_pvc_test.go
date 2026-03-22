//go:build checkpointfull
// +build checkpointfull

package checkpoint_test

import (
	"testing"

	"github.com/wlqtjl/PhoenixGPU/pkg/checkpoint"
	"go.uber.org/zap/zaptest"
)

func TestLocalPVCBackend_Contract(t *testing.T) {
	root := t.TempDir()
	logger := zaptest.NewLogger(t)

	backend, err := checkpoint.NewLocalPVCBackend(root, logger)
	if err != nil {
		t.Fatalf("NewLocalPVCBackend: %v", err)
	}

	// Run the full contract test suite against the real PVC implementation
	contractTest(t, backend)
}

func TestLocalPVCBackend_InvalidRoot(t *testing.T) {
	_, err := checkpoint.NewLocalPVCBackend("/proc/impossible/path/xyz", nil)
	if err == nil {
		t.Error("expected error when root dir cannot be created")
	}
}
