package diag

import (
	"path/filepath"
	"strings"

	"golang.org/x/tools/imports"
)

// FormatFile formats Go source exactly as goimports does: gofmt, plus
// adding missing imports (standard library and module packages) and
// removing unused ones, grouped and sorted. path locates the file's
// package and module. It returns the formatted source.
func (s *Sources) FormatFile(path string, src []byte) ([]byte, error) {
	return imports.Process(path, src, &imports.Options{Comments: true, TabIndent: true, TabWidth: 8, FormatOnly: false})
}

// IsGoFile reports whether name is a Go source file marmot formats.
func IsGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasPrefix(filepath.Base(name), ".")
}
