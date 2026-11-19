package driver

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Gaurav-Gosain/marmot/internal/codegen"
)

// homeInstances: whole program -release builds emit each large shared
// function once, in an instances module (codegen/home.go).
// MARMOT_HOME_INSTANCES=1 for now, until measured.
func (b *build) homeInstances() bool {
	return b.cfg.Release && !b.incr && os.Getenv("MARMOT_HOME_INSTANCES") == "1"
}

// addInstances generates the instances module after every other module
// and adds it as one more part (never cached: its contents follow from the
// whole build).
func (b *build) addInstances(cg *codegen.Program, n int) error {
	mod, ok, err := cg.GenerateInstances()
	if err != nil || !ok {
		return err
	}
	b.partMu.Lock()
	j := n + len(b.parts)
	b.parts = append(b.parts, codegen.Module{})
	b.partKeys = append(b.partKeys, "")
	b.partIDs = append(b.partIDs, instancesPart)
	b.partMu.Unlock()
	p := filepath.Join(b.work, fmt.Sprintf("m%03d.mlir", j))
	if err := writeModuleFile(p, nil, mod.Text.Segments()); err != nil {
		return err
	}
	b.partMu.Lock()
	b.parts[j-n] = codegen.Module{Name: mod.Name, Path: p}
	b.partMu.Unlock()
	if b.stream != nil {
		b.stream.send(j, p, "")
	}
	b.logf("instances module: %d bytes", mod.Text.Len())
	return nil
}
