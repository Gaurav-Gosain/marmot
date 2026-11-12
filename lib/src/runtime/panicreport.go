// Panic reports for people and programs. The plain trace printed by
// fatalpanic stays the default when standard error is not a terminal (tests
// and tools compare it). In a terminal the report shows the frames of the
// program with their source lines and folds runtime and library frames; with
// MARMOT_OUTPUT=json it is one JSON object (schema 1, kind panic).

package runtime

import "unsafe"

//go:linkname rt_isatty rt_isatty
func rt_isatty(fd int32) int32

//go:linkname rt_gstates rt_gstates
func rt_gstates(out *[3]int64)

//go:linkname rt_output_json rt_output_json
func rt_output_json() int32

const (
	panicPlain = iota
	panicHuman
	panicJSON
)

// panicMode picks the report form from MARMOT_OUTPUT (json, human, plain)
// and, when it is not set, from whether standard error is a terminal.
func panicMode() int {
	switch gogetenv("MARMOT_OUTPUT") {
	case "json":
		return panicJSON
	case "human":
		return panicHuman
	case "":
		if rt_isatty(2) != 0 {
			return panicHuman
		}
	}
	return panicPlain
}

// printCapture, when not nil, receives print output instead of standard
// error (used to turn a panic value into a string for JSON).
var printCapture *[]byte

// panicFrame is one logical frame of a report.
type panicFrame struct {
	info    pcInfo
	inlined bool
}

// collectFrames walks the stack from fp like printTraceback: the runtime
// frames of the panic machinery at the top are dropped.
func collectFrames(fp uintptr, out []panicFrame) int {
	var pcs [100]uintptr
	k := callers(fp, 0, pcs[:])
	leading := true
	n := 0
	var fr [maxInlineDepth]pcInfo
	for _, pc := range pcs[:k] {
		nf := pcFrames(pc-1, fr[:])
		for i, info := range fr[:nf] {
			if info.line == 0 && info.file == "" || leading && info.namePrefix == "runtime." {
				continue
			}
			leading = false
			if n < len(out) {
				out[n] = panicFrame{info, i < nf-1}
				n++
			}
		}
	}
	return n
}

// userFrame reports whether a frame belongs to the program rather than to
// the standard library or the runtime: package main, or a package whose
// path starts with a domain (a dot in the first element).
func userFrame(info pcInfo) bool {
	p := info.namePrefix
	if len(p) >= 5 && p[:5] == "main." {
		return true
	}
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '/':
			return false
		case '.':
			return i < len(p)-1 // a dot before the first slash, not the final one
		}
	}
	return false
}

// framePkg returns the package path of a frame.
func framePkg(info pcInfo) string {
	p := info.namePrefix
	if len(p) > 0 && p[len(p)-1] == '.' {
		return p[:len(p)-1]
	}
	return p
}

// panicColors are the escapes of the human report.
type panicColors struct {
	red, bold, dim, blue, cyan, reset string
}

func reportColors() panicColors {
	if gogetenv("NO_COLOR") != "" || gogetenv("TERM") == "dumb" || rt_isatty(2) == 0 {
		return panicColors{}
	}
	return panicColors{"\x1b[1;31m", "\x1b[1m", "\x1b[2m", "\x1b[1;34m", "\x1b[36m", "\x1b[0m"}
}

// sourceCache holds the last source file read for the report.
var sourceCache struct {
	path string
	data []byte
}

// sourceLine returns line n of file, or "" when the file cannot be read.
func sourceLine(file string, n int) string {
	if sourceCache.path != file {
		sourceCache.path = file
		sourceCache.data = readSmallFile(file)
	}
	d := sourceCache.data
	line := 1
	start := 0
	for i := 0; i < len(d); i++ {
		if d[i] == '\n' {
			if line == n {
				return string(d[start:i])
			}
			line++
			start = i + 1
		}
	}
	if line == n && start < len(d) {
		return string(d[start:])
	}
	return ""
}

// readSmallFile reads up to 4 MB of a file.
func readSmallFile(path string) []byte {
	p := make([]byte, len(path)+1)
	copy(p, path)
	fd := rt_open(unsafe.Pointer(&p[0]), 0, 0)
	if fd < 0 {
		return nil
	}
	var out []byte
	buf := make([]byte, 64<<10)
	for len(out) < 4<<20 {
		n := rt_read(int32(fd), unsafe.Pointer(&buf[0]), uintptr(len(buf)))
		if n <= 0 {
			break
		}
		out = append(out, buf[:n]...)
	}
	rt_close(int32(fd))
	return out
}

// expandTabs replaces tabs with spaces to a width of 4.
func expandTabs(s string) string {
	var b []byte
	col := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			for {
				b = append(b, ' ')
				col++
				if col%4 == 0 {
					break
				}
			}
			continue
		}
		b = append(b, s[i])
		col++
	}
	return string(b)
}

// firstNonSpace returns the display column of the first character that is
// not white space.
func firstNonSpace(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' {
			return i
		}
	}
	return 0
}

// cwdPrefix is the working directory with a trailing slash, for showing
// paths relative to it.
func cwdPrefix() string {
	if d := gogetenv("PWD"); d != "" {
		if d[len(d)-1] != '/' {
			d += "/"
		}
		return d
	}
	return ""
}

func shortPath(full, cwd string) string {
	if cwd != "" && len(full) > len(cwd) && full[:len(cwd)] == cwd {
		return "./" + full[len(cwd):]
	}
	return full
}

// panicMessage returns the text of one panic value as print would show it.
func panicMessage(p *_panic) string {
	if p.hasMsg {
		return p.msg
	}
	var buf []byte
	printCapture = &buf
	printpanicval(p.arg)
	printCapture = nil
	return string(buf)
}

// printPanicHuman prints the terminal report of a fatal panic.
func printPanicHuman(p *_panic, goid int64, fp uintptr) {
	c := reportColors()
	printPanicChain(p, c)
	var frames [64]panicFrame
	n := collectFrames(fp, frames[:])
	cwd := cwdPrefix()
	printstring("\n")
	printstring(c.bold)
	printstring("goroutine ")
	printint(goid)
	printstring(c.reset)
	printstring(" [running]:\n")
	for i := 0; i < n; {
		f := frames[i]
		if !userFrame(f.info) {
			// Fold a run of library and runtime frames into one line.
			j := i
			var pkgs [4]string
			np := 0
			for j < n && !userFrame(frames[j].info) {
				pk := framePkg(frames[j].info)
				dup := false
				for _, q := range pkgs[:np] {
					if q == pk {
						dup = true
					}
				}
				if !dup && np < len(pkgs) {
					pkgs[np] = pk
					np++
				}
				j++
			}
			printstring(c.dim)
			printstring("  ... ")
			printint(int64(j - i))
			if j-i == 1 {
				printstring(" frame in ")
			} else {
				printstring(" frames in ")
			}
			for k, q := range pkgs[:np] {
				if k > 0 {
					printstring(", ")
				}
				printstring(q)
			}
			printstring(c.reset)
			printstring("\n")
			i = j
			continue
		}
		file := f.info.filePrefix + f.info.file
		printstring("  ")
		printstring(c.bold)
		printstring(f.info.namePrefix)
		printstring(f.info.name)
		printstring(c.reset)
		if f.inlined {
			printstring(c.dim)
			printstring(" (inlined)")
			printstring(c.reset)
		}
		printstring("\n      ")
		printstring(c.cyan)
		printstring(shortPath(file, cwd))
		printstring(":")
		printint(int64(f.info.line))
		printstring(c.reset)
		printstring("\n")
		if src := sourceLine(file, f.info.line); src != "" {
			src = expandTabs(src)
			ind := firstNonSpace(src)
			ln := itoa(int64(f.info.line))
			printstring(c.blue)
			for k := len(ln); k < 6; k++ {
				printstring(" ")
			}
			printstring(ln)
			printstring(" | ")
			printstring(c.reset)
			printstring(src[ind:])
			printstring("\n")
			if firstUser(frames[:i]) {
				// Mark the line where the panic happened.
				printstring(c.blue)
				printstring("       | ")
				printstring(c.reset)
				printstring(c.red)
				for k := ind; k < len(src); k++ {
					printstring("^")
				}
				printstring(" panicked here")
				printstring(c.reset)
				printstring("\n")
			}
		}
		i++
	}
	printGoroutineSummary(c)
	printstring(c.dim)
	printstring("exit status 2 (MARMOT_OUTPUT=plain prints the plain trace)")
	printstring(c.reset)
	printstring("\n")
}

// firstUser reports whether no frame in fs is a user frame, so the next
// one is the innermost user frame.
func firstUser(fs []panicFrame) bool {
	for _, f := range fs {
		if userFrame(f.info) {
			return false
		}
	}
	return true
}

func printPanicChain(p *_panic, c panicColors) {
	if p.link != nil {
		printPanicChain(p.link, c)
		if p.link.repanicked {
			return
		}
	}
	if p.goexit {
		return
	}
	printstring(c.red)
	printstring("panic: ")
	printstring(c.reset)
	printstring(c.bold)
	if p.hasMsg {
		printindented(p.msg)
	} else {
		printpanicval(p.arg)
	}
	printstring(c.reset)
	if p.recovered && p.repanicked {
		printstring(" [recovered, repanicked]")
	} else if p.recovered {
		printstring(" [recovered]")
	}
	printstring("\n")
}

func printGoroutineSummary(c panicColors) {
	var st [3]int64
	rt_gstates(&st)
	total := st[0] + st[1] + st[2]
	if total <= 1 {
		return
	}
	printstring(c.dim)
	printstring("\n")
	printint(total)
	printstring(" goroutines: ")
	sep := false
	for i, name := range [3]string{"running", "runnable", "waiting"} {
		if st[i] == 0 {
			continue
		}
		if sep {
			printstring(", ")
		}
		sep = true
		printint(st[i])
		printstring(" ")
		printstring(name)
	}
	printstring(c.reset)
	printstring("\n")
}

// printPanicJSON prints the JSON report of a fatal panic on one line.
func printPanicJSON(p *_panic, goid int64, fp uintptr) {
	// The innermost panic is the one that ends the program.
	printstring(`{"schema":1,"kind":"panic","code":"M0020","message":`)
	printJSONString(panicMessage(p))
	printstring(`,"valueType":`)
	if t := efaceOf(&p.arg).typ; t != nil {
		printJSONString(t.str)
	} else {
		printstring(`"nil"`)
	}
	if p.link != nil {
		printstring(`,"chain":[`)
		first := true
		for q := p.link; q != nil; q = q.link {
			if q.goexit {
				continue
			}
			if !first {
				printstring(",")
			}
			first = false
			printJSONString(panicMessage(q))
		}
		printstring("]")
	}
	printstring(`,"goroutine":`)
	printint(goid)
	printstring(`,"frames":[`)
	var frames [64]panicFrame
	n := collectFrames(fp, frames[:])
	for i, f := range frames[:n] {
		if i > 0 {
			printstring(",")
		}
		printstring(`{"func":`)
		printJSONString(f.info.namePrefix + f.info.name)
		printstring(`,"file":`)
		printJSONString(f.info.filePrefix + f.info.file)
		printstring(`,"line":`)
		printint(int64(f.info.line))
		printstring(`,"col":0,"inlined":`)
		printbool(f.inlined)
		printstring(`,"user":`)
		printbool(userFrame(f.info))
		printstring("}")
	}
	var st [3]int64
	rt_gstates(&st)
	printstring(`],"goroutines":{"total":`)
	printint(st[0] + st[1] + st[2])
	printstring(`,"running":`)
	printint(st[0])
	printstring(`,"runnable":`)
	printint(st[1])
	printstring(`,"waiting":`)
	printint(st[2])
	printstring(`},"exitCode":2}`)
	printstring("\n")
}

// printJSONString prints s as a JSON string literal.
func printJSONString(s string) {
	const hex = "0123456789abcdef"
	var b []byte
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
		case c == '\n':
			b = append(b, '\\', 'n')
		case c == '\t':
			b = append(b, '\\', 't')
		case c < 0x20:
			b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&15])
		default:
			b = append(b, c)
		}
	}
	b = append(b, '"')
	gwriteBytes(b)
}
