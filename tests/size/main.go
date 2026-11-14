// Command size is the binary size scoreboard: it builds programs with go
// (default and -ldflags="-s -w") and with marmot (dev, -O 2, -release,
// -release -s), and writes each binary's file bytes by kind (code, read only
// data, data, pc table, type data, DWARF, symbols and the rest) as a
// Markdown report and a JSON record.
//
//	go run ./tests/size                          # every tests/world program
//	go run ./tests/size -tuios ../tuios           # and tuios's cmd/tuios
//	go run ./tests/size -only 'cobra|yaml' -modes dev,release-s
//
// The goal it tracks: every marmot mode smaller than go (dev and -O 2 and
// -release against go build, -release -s against go -s -w). Run it from
// the repository root; marmot comes from -marmot (default build/bin/marmot)
// and uses the environment as it is (MARMOT_ROOT and the rest).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/binsize"
)

var (
	flMarmot = flag.String("marmot", "build/bin/marmot", "the marmot to measure")
	flTuios  = flag.String("tuios", "", "a tuios checkout: also measure its ./cmd/tuios (built with CGO_ENABLED=0)")
	flWorld  = flag.String("world", "tests/world", "the world programs")
	flOnly   = flag.String("only", "", "regexp: only the programs whose name matches")
	flModes  = flag.String("modes", "go,go-sw,dev,O2,release,release-s", "the builds, comma separated")
	flOut    = flag.String("o", "docs/status/size.md", "the Markdown report")
	flJSON   = flag.String("json", "docs/status/size.json", "the JSON record")
)

// modes are the builds: the go command or marmot, and the flags.
var modes = map[string]struct {
	marmot bool
	flags  []string
	label  string
}{
	"go":        {false, nil, "go"},
	"go-sw":     {false, []string{"-ldflags=-s -w"}, "go -s -w"},
	"dev":       {true, nil, "marmot dev"},
	"O2":        {true, []string{"-O", "2"}, "marmot -O 2"},
	"release":   {true, []string{"-release"}, "marmot -release"},
	"release-s": {true, []string{"-release", "-s"}, "marmot -release -s"},
}

// against is the go build each marmot mode must be smaller than.
var against = map[string]string{"dev": "go", "O2": "go", "release": "go", "release-s": "go-sw"}

// Build is one binary.
type Build struct {
	Mode    string           `json:"mode"`
	OK      bool             `json:"ok"`
	Error   string           `json:"error,omitempty"`
	File    int64            `json:"file"`
	Kinds   map[string]int64 `json:"kinds,omitempty"`
	Seconds float64          `json:"seconds"`
}

// Program is one program and its builds.
type Program struct {
	Name   string            `json:"name"`
	Builds map[string]*Build `json:"builds"`
}

// Record is one run of the scoreboard.
type Record struct {
	Date     string     `json:"date"`
	Commit   string     `json:"commit"`
	Platform string     `json:"platform"`
	Programs []*Program `json:"programs"`
}

// kindOrder is the order of the kinds in the report.
var kindOrder = []string{"code", "rodata", "data", "pc table", "types", "DWARF", "symbols", "other"}

// kindOf is the kind of a section's bytes, or "" for a section that takes
// no file space (bss).
func kindOf(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "bss"), strings.Contains(n, "common"), strings.Contains(n, "tdata") && strings.Contains(n, "thread"):
		return ""
	case strings.Contains(n, "pcln") || strings.Contains(n, "pclntab"):
		return "pc table"
	case strings.Contains(n, "debug") || strings.Contains(n, "dwarf"):
		return "DWARF"
	case strings.Contains(n, "go_type") || strings.Contains(n, "typelink") || strings.Contains(n, "tlinks") || strings.Contains(n, "itablink") || strings.Contains(n, "go.type"):
		return "types"
	case strings.Contains(n, "symtab") || strings.Contains(n, "strtab"):
		return "symbols"
	case strings.Contains(n, "rodata") || strings.Contains(n, "const") || strings.Contains(n, "cstring") || strings.Contains(n, "literal") || strings.Contains(n, "gcc_except") || strings.Contains(n, "eh_frame") || strings.Contains(n, "unwind"):
		return "rodata"
	case strings.Contains(n, "text") || strings.Contains(n, "stubs") || strings.Contains(n, "plt"):
		return "code"
	case strings.Contains(n, "data") || strings.Contains(n, "got") || strings.Contains(n, "init_array") || strings.Contains(n, "fini_array"):
		return "data"
	}
	return "other"
}

func kinds(path string) (map[string]int64, error) {
	b, err := binsize.Open(path)
	if err != nil {
		return nil, err
	}
	k := map[string]int64{}
	var sum int64
	for _, s := range b.Sections {
		kind := kindOf(s.Name)
		if kind == "" {
			continue
		}
		k[kind] += int64(s.Size)
		sum += int64(s.Size)
	}
	// The rest of the file: on Mach-O the symbol and string tables, the
	// code signature and the headers (__LINKEDIT has no sections).
	if rest := b.FileSize - sum; rest > 0 {
		if b.Format == "macho" {
			k["symbols"] += rest
		} else {
			k["other"] += rest
		}
	}
	return k, nil
}

func build(dir, pkg, mode string, env []string, out string) *Build {
	m := modes[mode]
	var cmd *exec.Cmd
	if m.marmot {
		marmot, _ := filepath.Abs(*flMarmot)
		cmd = exec.Command(marmot, append(append([]string{"build"}, m.flags...), "-o", out, pkg)...)
	} else {
		cmd = exec.Command("go", append(append([]string{"build"}, m.flags...), "-o", out, pkg)...)
	}
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "MARMOT_OUTPUT=plain", "GOPROXY=off"), env...)
	t0 := time.Now()
	outb, err := cmd.CombinedOutput()
	r := &Build{Mode: mode, Seconds: time.Since(t0).Seconds()}
	if err != nil {
		msg := strings.TrimSpace(string(outb))
		if lines := strings.Split(msg, "\n"); len(lines) > 3 {
			msg = strings.Join(lines[len(lines)-3:], "\n")
		}
		r.Error = msg
		return r
	}
	st, err := os.Stat(out)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.File = st.Size()
	r.Kinds, err = kinds(out)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.OK = true
	return r
}

func main() {
	flag.Parse()
	var only *regexp.Regexp
	if *flOnly != "" {
		only = regexp.MustCompile(*flOnly)
	}
	want := strings.Split(*flModes, ",")
	for _, m := range want {
		if _, ok := modes[m]; !ok {
			fmt.Fprintf(os.Stderr, "size: unknown mode %q\n", m)
			os.Exit(2)
		}
	}
	tmp, err := os.MkdirTemp("", "marmot-size-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "size:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	type job struct {
		name, dir, pkg string
		env            []string
	}
	var jobs []job
	if *flTuios != "" {
		dir, _ := filepath.Abs(*flTuios)
		jobs = append(jobs, job{"tuios", dir, "./cmd/tuios", []string{"CGO_ENABLED=0", "GOFLAGS=-mod=readonly"}})
	}
	ents, _ := os.ReadDir(*flWorld)
	for _, e := range ents {
		dir, _ := filepath.Abs(filepath.Join(*flWorld, e.Name()))
		if !e.IsDir() || !exists(filepath.Join(dir, "main.go")) {
			continue
		}
		var env []string
		if exists(filepath.Join(dir, "vendor")) {
			env = append(env, "GOFLAGS=-mod=vendor")
		}
		jobs = append(jobs, job{e.Name(), dir, ".", env})
	}
	rec := &Record{Date: time.Now().Format("2006-01-02 15:04"), Commit: commit(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
	for _, j := range jobs {
		if only != nil && !only.MatchString(j.name) {
			continue
		}
		p := &Program{Name: j.name, Builds: map[string]*Build{}}
		for _, m := range want {
			b := build(j.dir, j.pkg, m, j.env, filepath.Join(tmp, j.name+"-"+m))
			p.Builds[m] = b
			status := fmt.Sprintf("%d bytes", b.File)
			if !b.OK {
				status = "FAILED"
			}
			fmt.Fprintf(os.Stderr, "%-20s %-10s %6.1fs %s\n", j.name, m, b.Seconds, status)
			os.Remove(filepath.Join(tmp, j.name+"-"+m))
		}
		rec.Programs = append(rec.Programs, p)
	}
	if err := os.WriteFile(*flOut, []byte(report(rec, want)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "size:", err)
		os.Exit(1)
	}
	data, _ := json.MarshalIndent(rec, "", "  ")
	if err := os.WriteFile(*flJSON, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "size:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "size: wrote %s and %s\n", *flOut, *flJSON)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func commit() string {
	out, err := exec.Command("git", "rev-parse", "--short=8", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func mb(n int64) string {
	switch {
	case n >= 10<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
}

func report(rec *Record, want []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Binary size scoreboard\n\n")
	fmt.Fprintf(&b, "%s, commit %s, %s. File sizes (MB = 2^20 bytes). Goal: every marmot mode smaller than go: dev, -O 2 and -release against `go build`, -release -s against `go build -ldflags=\"-s -w\"`. A marmot cell shows its size against that go build; **over** marks a miss. Generated by `go run ./tests/size`.\n\n", rec.Date, rec.Commit, rec.Platform)
	b.WriteString("| program |")
	for _, m := range want {
		fmt.Fprintf(&b, " %s |", modes[m].label)
	}
	b.WriteString("\n|---|")
	for range want {
		b.WriteString("---:|")
	}
	b.WriteString("\n")
	met, total := 0, 0
	for _, p := range rec.Programs {
		fmt.Fprintf(&b, "| %s |", p.Name)
		for _, m := range want {
			x := p.Builds[m]
			if x == nil || !x.OK {
				b.WriteString(" failed |")
				continue
			}
			cell := mb(x.File)
			if ref, ok := against[m]; ok {
				if g := p.Builds[ref]; g != nil && g.OK {
					total++
					r := float64(x.File) / float64(g.File)
					if x.File < g.File {
						met++
						cell += fmt.Sprintf(" (%.2fx)", r)
					} else {
						cell += fmt.Sprintf(" (%.2fx, **over**)", r)
					}
				}
			}
			fmt.Fprintf(&b, " %s |", cell)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n%d of %d marmot builds are smaller than their go build.\n", met, total)

	// By kind: tuios on its own, and the world programs summed.
	section := func(title string, ps []*Program) {
		if len(ps) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n## %s by kind\n\n| kind |", title)
		for _, m := range want {
			fmt.Fprintf(&b, " %s |", modes[m].label)
		}
		b.WriteString("\n|---|")
		for range want {
			b.WriteString("---:|")
		}
		b.WriteString("\n")
		rows := append(append([]string{}, kindOrder...), "file")
		for _, k := range rows {
			fmt.Fprintf(&b, "| %s |", k)
			for _, m := range want {
				var sum int64
				n := 0
				for _, p := range ps {
					if x := p.Builds[m]; x != nil && x.OK {
						n++
						if k == "file" {
							sum += x.File
						} else {
							sum += x.Kinds[k]
						}
					}
				}
				if n == 0 {
					b.WriteString(" |")
					continue
				}
				fmt.Fprintf(&b, " %s |", mb(sum))
			}
			b.WriteString("\n")
		}
	}
	var tuios, world []*Program
	for _, p := range rec.Programs {
		if p.Name == "tuios" {
			tuios = append(tuios, p)
		} else {
			world = append(world, p)
		}
	}
	section("tuios", tuios)
	section(fmt.Sprintf("World programs (%d, summed)", len(world)), world)

	var fails []string
	for _, p := range rec.Programs {
		ms := make([]string, 0, len(p.Builds))
		for m := range p.Builds {
			ms = append(ms, m)
		}
		sort.Strings(ms)
		for _, m := range ms {
			if x := p.Builds[m]; !x.OK {
				fails = append(fails, fmt.Sprintf("- %s, %s: %s", p.Name, modes[m].label, strings.ReplaceAll(x.Error, "\n", " / ")))
			}
		}
	}
	if len(fails) > 0 {
		b.WriteString("\n## Failed builds\n\n" + strings.Join(fails, "\n") + "\n")
	}
	b.WriteString("\nKinds: code is the text sections; rodata the read only data (constants, strings, unwind tables); data the writable data with file bytes (bss takes none); pc table marmot's __marmot_pcln or go's gopclntab; types go's type sections and marmot's type links; symbols the symbol and string tables (on Mach-O the rest of __LINKEDIT: code signature and headers too).\n")
	return b.String()
}
