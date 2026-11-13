package diag

import (
	"regexp"
	"strings"
)

// cgo in diagnostics: the type checker sees cgo's generated names
// (_Ctype_int, _Cfunc_add); they are shown as the user wrote them (C.int,
// C.add), and a mismatch between a Go and a C type gets the conversion
// that fixes it.

var (
	reCgoFunc  = regexp.MustCompile(`\(?_Cfunc_(\w+)\)?`)
	reCgoName  = regexp.MustCompile(`_C(?:type|var|iconst|fpvar|macro)_(\w+)`)
	reCgoTypeQ = regexp.MustCompile("`?_Ctype_(\\w+)`?")
	reCgoGoToC = regexp.MustCompile(`^cannot use (.+) \((?:variable|value|constant)(?: of (?:type|\w+ type))? ([\w.*\[\]]+)\) as (\*?C\.\w+) value in (?:argument to|assignment|variable declaration|return statement)`)
	reCgoCToGo = regexp.MustCompile(`^cannot use (.+) \((?:variable|value)(?: of (?:type|\w+ type))? (C\.\w+)\) as ([\w.]+) value in`)
	goNumeric  = map[string]bool{"int": true, "int8": true, "int16": true, "int32": true, "int64": true, "uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true, "float32": true, "float64": true, "byte": true, "rune": true, "untyped int": true, "untyped float": true}
	cNumeric   = map[string]bool{"C.char": true, "C.schar": true, "C.uchar": true, "C.short": true, "C.ushort": true, "C.int": true, "C.uint": true, "C.long": true, "C.ulong": true, "C.longlong": true, "C.ulonglong": true, "C.float": true, "C.double": true, "C.size_t": true, "C.ssize_t": true, "C.int8_t": true, "C.int16_t": true, "C.int32_t": true, "C.int64_t": true, "C.uint8_t": true, "C.uint16_t": true, "C.uint32_t": true, "C.uint64_t": true}
)

// demangleCgo rewrites cgo's generated names in s as Go code spells them.
func demangleCgo(s string) string {
	if !strings.Contains(s, "_C") {
		return s
	}
	s = reCgoFunc.ReplaceAllString(s, "C.$1")
	s = strings.ReplaceAll(s, "_Ctype_struct_", "C.struct_")
	return reCgoName.ReplaceAllString(s, "C.$1")
}

// cgoHelp adds the conversion for a Go value used as a C one (or the
// reverse): C.int(x), C.CString(s), int(r).
func cgoHelp(d *Diagnostic, src *Sources) {
	if d.Code != CodeCannotUse && d.Code != CodeMismatched {
		return
	}
	expr := func(e string) string { return strings.TrimSpace(e) }
	if m := reCgoGoToC.FindStringSubmatch(d.Message); m != nil {
		e, goT, cT := expr(m[1]), m[2], m[3]
		switch {
		case cT == "*C.char" && goT == "string":
			d.Help = append(d.Help, "C wants a C string: `cs := C.CString("+e+")`, pass `cs`, and free it with `defer C.free(unsafe.Pointer(cs))` (the preamble needs `#include <stdlib.h>`, the file `import \"unsafe\"`)")
		case strings.HasPrefix(cT, "*C.") && goT != "" && !strings.HasPrefix(goT, "*"):
			d.Help = append(d.Help, "C wants a pointer: pass `&"+e+"` if `"+e+"` has the type `"+strings.TrimPrefix(cT, "*")+"`")
		case cNumeric[cT] && (goNumeric[goT] || goT == "bool"):
			conv := cT + "(" + e + ")"
			fixed := false
			defer func() {
				if !fixed {
					d.Help = append(d.Help, "convert to the C type: `"+conv+"`")
				}
			}()
			if d.Span.Valid() && src != nil {
				if f := src.file(d.Span.File); f != nil {
					if l, ok := f.line(d.Span.Line); ok && d.Span.Col >= 1 && d.Span.Col-1+len(e) <= len(l) && l[d.Span.Col-1:d.Span.Col-1+len(e)] == e {
						fixed = true
						d.Fixes = append(d.Fixes, Fix{Title: "convert to `" + conv + "`", Applicability: MachineApplicable,
							Edits: []Edit{{File: d.Span.File, Span: Span{File: d.Span.File, Line: d.Span.Line, Col: d.Span.Col, EndLine: d.Span.Line, EndCol: d.Span.Col + len(e)}, NewText: conv}}})
					}
				}
			}
		}
		return
	}
	if m := reCgoCToGo.FindStringSubmatch(d.Message); m != nil {
		e, cT, goT := expr(m[1]), m[2], m[3]
		switch {
		case cT == "C.char" || goT == "string":
			if goT == "string" {
				d.Help = append(d.Help, "a C string becomes a Go string with `C.GoString(...)` (or `C.GoStringN(p, n)` for a length)")
			}
		case cNumeric[cT] && goNumeric[goT]:
			d.Help = append(d.Help, "convert to the Go type: `"+goT+"("+e+")`")
		}
	}
}
