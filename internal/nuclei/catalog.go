package nuclei

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/projectdiscovery/nuclei/v3/pkg/catalog/disk"
	"github.com/projectdiscovery/nuclei/v3/pkg/protocols/common/interactsh"
)

// Apply the same Interactsh exclusion to direct templates and workflow children.
type localCatalog struct {
	*disk.DiskCatalog
	mu      sync.Mutex
	skipped map[string]bool
}

func (c *localCatalog) GetTemplatePath(path string) ([]string, error) {
	paths, err := c.DiskCatalog.GetTemplatePath(path)
	if err != nil {
		return nil, err
	}
	filtered, failures := c.filter(paths)
	var errs []error
	for _, err := range failures {
		errs = append(errs, err)
	}
	return filtered, errors.Join(errs...)
}

func (c *localCatalog) GetTemplatesPath(paths []string) ([]string, map[string]error) {
	all, failures := c.DiskCatalog.GetTemplatesPath(paths)
	filtered, readErrors := c.filter(all)
	if failures == nil {
		failures = make(map[string]error)
	}
	for path, err := range readErrors {
		failures[path] = err
	}
	return filtered, failures
}

func (c *localCatalog) filter(paths []string) ([]string, map[string]error) {
	var filtered []string
	failures := make(map[string]error)
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" || ext == ".json" {
			data, err := os.ReadFile(path)
			if err != nil {
				failures[path] = err
				continue
			}
			if interactsh.HasMarkers(string(data)) {
				c.mu.Lock()
				c.skipped[path] = true
				c.mu.Unlock()
				continue
			}
		}
		filtered = append(filtered, path)
	}
	return filtered, failures
}

func (c *localCatalog) skippedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.skipped)
}
