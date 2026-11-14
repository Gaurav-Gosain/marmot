package codegen

import (
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/target"
)

// sizes is the target's layout (target.Sizes), also for a Program made
// without NewProgram (tests).
func (p *Program) sizes() types.Sizes {
	if p.opts.Sizes == nil {
		return target.Sizes(p.opts.GOARCH)
	}
	return p.opts.Sizes
}

// ptrSize is the size of a pointer on the target (4 on wasm, else 8).
func (p *Program) ptrSize() int64 { return target.PtrSize(p.opts.GOARCH) }

// itabFun is the byte offset of method slot idx in an itab: after inter,
// typ (pointers), hash and flags (uint32 each), as runtime.itab.
func (p *Program) itabFun(idx int) int64 { return 2*p.ptrSize() + 8 + p.ptrSize()*int64(idx) }

// ptrAlign is the alignment attribute of a pointer load or store.
func (f *funcGen) ptrAlign() string {
	if f.m.p.ptrSize() == 4 {
		return "{alignment = 4 : i64}"
	}
	return "{alignment = 8 : i64}"
}
