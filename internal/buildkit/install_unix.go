//go:build !windows

package buildkit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func install(candidate, target, stage string, log io.Writer) (bool, error) {
	if err := os.Rename(candidate, target); err != nil {
		return false, fmt.Errorf("replace executable: %w", err)
	}
	if parent, err := os.Open(filepath.Dir(target)); err == nil {
		parent.Sync()
		parent.Close()
	}
	return false, nil
}
