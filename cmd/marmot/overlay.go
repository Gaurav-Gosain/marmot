package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// readOverlay reads a go build -overlay file: {"Replace": {path: file}}.
// Paths relative to the current directory, as go reads them. The result
// maps each absolute path to the contents of its file. An empty file
// (deleting path) is not supported.
func readOverlay(name string) (map[string][]byte, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var o struct{ Replace map[string]string }
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("overlay %s: %v", name, err)
	}
	out := make(map[string][]byte, len(o.Replace))
	for path, src := range o.Replace {
		if src == "" {
			return nil, fmt.Errorf("overlay %s: deleting %s is not supported", name, path)
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("overlay %s: %v", name, err)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		out[abs] = b
	}
	return out, nil
}
