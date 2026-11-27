package driver

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/loader"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"golang.org/x/tools/go/packages"
)

// Build info: the text runtime/debug.ReadBuildInfo returns and go version -m
// reads, written as the go command writes it (cmd/go/internal/load,
// setBuildInfo): the Go version, the main package path, the main module, the
// modules the program uses, and the build settings, with the version control
// state of the main module under -buildvcs. marmot adds marmot.version, the
// commit of the marmot that built the program; -compiler stays gc, as
// runtime.Compiler says. Not written: DefaultGODEBUG, since marmot programs
// do not apply the GODEBUG defaults of the go.mod go version.
//
// The text goes into a small object made at link time (buildInfoObject), so
// no package or module key depends on it: a new commit relinks and compiles
// nothing again. The up-to-date stamp includes the version control state.

// The markers around the text, as cmd/go/internal/modload writes them;
// runtime/debug.ReadBuildInfo strips 16 bytes from each end.
const (
	modinfoStart = "\x30\x77\xaf\x0c\x92\x74\x08\x02\x41\xe1\xc1\x07\xe6\xd6\x18\xe6"
	modinfoEnd   = "\xf9\x32\x43\x31\x86\x18\x20\x72\x00\x82\x42\x10\x41\x16\xd8\xf2"
)

// vcsState is the version control state of the main module's repository.
type vcsState struct {
	rev      string    // full commit hash
	time     time.Time // commit time
	modified bool      // uncommitted changes, untracked files included (as cmd/go)
	top      string    // repository root
}

// buildVCS reports whether this build stamps version control state:
// -buildvcs=true always, false never, auto (the default) for a main
// package that is not a test and is in a git repository.
func (b *build) buildVCS() string {
	switch v := b.cfg.BuildVCS; v {
	case "true", "false":
		return v
	}
	return "auto"
}

// gitState reads the state of the git repository holding dir. ok is false
// when dir is not in one or git is missing.
func gitState(dir string) (s vcsState, ok bool) {
	git := func(args ...string) (string, error) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
		out, err := c.Output()
		return strings.TrimSpace(string(out)), err
	}
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return s, false
	}
	line, err := git("-c", "log.showsignature=false", "log", "-1", "--format=%H:%ct")
	if err != nil {
		return s, false // a repository without commits
	}
	rev, ct, found := strings.Cut(line, ":")
	sec, perr := strconv.ParseInt(ct, 10, 64)
	if !found || perr != nil {
		return s, false
	}
	st, err := git("status", "--porcelain")
	if err != nil {
		return s, false
	}
	return vcsState{rev: rev, time: time.Unix(sec, 0).UTC(), modified: st != "", top: top}, true
}

// stampVCSKey is the version control part of the up-to-date stamp: a build
// whose commit or working tree state changed must not reuse an executable.
func (b *build) stampVCSKey() string {
	if b.buildVCS() == "false" || b.cfg.Test {
		return "vcs off"
	}
	s, ok := gitState(b.cfg.Dir)
	if !ok {
		return "vcs none"
	}
	return fmt.Sprintf("vcs %s %d %v", s.rev, s.time.Unix(), s.modified)
}

var goVersionRE = regexp.MustCompile(`var buildVersion = "([^"]+)"`)

// goVersion is runtime.Version() of the programs marmot builds (the
// runtime's buildVersion), so ReadBuildInfo's GoVersion matches it.
func goVersion(goroot string) string {
	src, err := os.ReadFile(filepath.Join(goroot, "src", "runtime", "extern.go"))
	if err == nil {
		if m := goVersionRE.FindSubmatch(src); m != nil {
			return string(m[1])
		}
	}
	return "go1"
}

// marmotVersion is the commit of this marmot binary, from its own build info.
func marmotVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if dirty {
				rev += "+dirty"
			}
			return rev
		}
	}
	return "devel"
}

// readSums reads a go.sum file: "path version" to its h1 hash.
func readSums(file string) map[string]string {
	sums := map[string]string{}
	f, err := os.Open(file)
	if err != nil {
		return sums
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) == 3 && !strings.HasSuffix(fs[1], "/go.mod") {
			sums[fs[0]+" "+fs[1]] = fs[2]
		}
	}
	return sums
}

// buildInfoOn reports MARMOT_BUILDINFO=1: stamp build info (opt-in until
// its gate). Without it the runtime and the link are as before: no build
// info object, and debug_modinfo returns the constant "" (lib/src/runtime
// modinfo_off.go).
func buildInfoOn() bool { return os.Getenv("MARMOT_BUILDINFO") == "1" }

// buildInfoText returns the build info of the program (with its markers),
// or "" for a build that carries none: MARMOT_BUILDINFO unset, not an
// executable, not a main package, or a test binary.
func (b *build) buildInfoText(lp *loader.Program) string {
	if !buildInfoOn() || b.cfg.Emit != "exe" || b.cfg.Test || len(lp.Packages) != 1 || lp.Packages[0].Name != "main" || b.goarch == "wasm" {
		return ""
	}
	root := lp.Packages[0]
	bi := &debug.BuildInfo{GoVersion: goVersion(lp.GOROOT), Path: root.PkgPath}
	var sums map[string]string
	if m := root.Module; m != nil {
		bi.Main = debug.Module{Path: m.Path, Version: "(devel)"}
		sums = readSums(filepath.Join(m.Dir, "go.sum"))
	}
	bi.Deps = buildDeps(lp.All, root.Module, sums)

	set := func(k, v string) { bi.Settings = append(bi.Settings, debug.BuildSetting{Key: k, Value: v}) }
	mode := b.cfg.BuildMode
	if mode == "" {
		mode = "exe"
	}
	set("-buildmode", mode)
	set("-compiler", "gc")
	if len(b.cfg.Tags) > 0 {
		set("-tags", strings.Join(b.cfg.Tags, ","))
	}
	if b.cfg.TrimPath {
		set("-trimpath", "true")
	}
	cgo := os.Getenv("CGO_ENABLED")
	if cgo == "" {
		cgo = "1"
		if b.cross() {
			cgo = "0"
		}
	}
	set("CGO_ENABLED", cgo)
	// As the go command: no CGO_ flags under -trimpath, they may hold paths.
	if cgo == "1" && !b.cfg.TrimPath {
		for _, k := range []string{"CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS"} {
			set(k, os.Getenv(k))
		}
	}
	set("GOARCH", b.goarch)
	if v := os.Getenv("GOEXPERIMENT"); v != "" {
		set("GOEXPERIMENT", v)
	}
	set("GOOS", b.goos)
	switch b.goarch {
	case "amd64":
		set("GOAMD64", envOr("GOAMD64", "v1"))
	case "arm64":
		set("GOARM64", envOr("GOARM64", "v8.0"))
	}
	if root.Module != nil && b.buildVCS() != "false" {
		dir := root.Module.Dir
		if s, ok := gitState(dir); ok && inDir(s.top, dir) {
			set("vcs", "git")
			set("vcs.revision", s.rev)
			set("vcs.time", s.time.Format(time.RFC3339))
			set("vcs.modified", strconv.FormatBool(s.modified))
			bi.Main.Version = vcsModVersion(root.Module, s)
		}
	}
	set("marmot.version", marmotVersion())
	return modinfoStart + bi.String() + modinfoEnd
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// inDir reports whether dir is top or below it.
func inDir(top, dir string) bool {
	rel, err := filepath.Rel(top, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// buildDeps lists the modules of the packages the program uses, other than
// the main module and the standard library, sorted by path.
func buildDeps(all []*packages.Package, main *packages.Module, sums map[string]string) []*debug.Module {
	seen := map[string]bool{}
	var deps []*debug.Module
	for _, p := range all {
		m := p.Module
		if m == nil || (main != nil && m.Path == main.Path) || seen[m.Path] {
			continue
		}
		seen[m.Path] = true
		if m.Main {
			// Another module of the workspace (go.work), as the go command lists it.
			deps = append(deps, &debug.Module{Path: m.Path, Version: "(devel)"})
			continue
		}
		d := &debug.Module{Path: m.Path, Version: m.Version, Sum: sums[m.Path+" "+m.Version]}
		if r := m.Replace; r != nil {
			d.Sum = ""
			d.Replace = &debug.Module{Path: r.Path, Version: r.Version}
			if r.Version != "" {
				d.Replace.Sum = sums[r.Path+" "+r.Version]
			}
		}
		deps = append(deps, d)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Path < deps[j].Path })
	return deps
}

// vcsModVersion is the main module's version from version control, as the go
// command sets it since Go 1.24: a semantic version tag on the commit, or
// else a pseudo-version after the highest such tag below it (v0.0.0 without
// one), with +dirty for a modified tree. Tags of a module in a subdirectory
// carry that directory as a prefix.
func vcsModVersion(m *packages.Module, s vcsState) string {
	prefix := ""
	if rel, err := filepath.Rel(s.top, m.Dir); err == nil && rel != "." {
		prefix = filepath.ToSlash(rel) + "/"
	}
	// The go command takes the version from the repository only when the
	// module path is the path of the module at the repository root, plus
	// the subdirectory (cmd/go: LookupLocal of the root module path); else
	// the main module stays (devel).
	root := rootModPath(s.top)
	want := trimMajor(root)
	if prefix != "" {
		want += "/" + strings.TrimSuffix(prefix, "/")
	}
	if root == "" || trimMajor(m.Path) != want {
		return "(devel)"
	}
	git := func(args ...string) []string {
		out, err := exec.Command("git", append([]string{"-C", s.top}, args...)...).Output()
		if err != nil {
			return nil
		}
		return strings.Fields(string(out))
	}
	major := majorOf(m.Path)
	valid := func(tag string) (string, bool) {
		v, ok := strings.CutPrefix(tag, prefix)
		if !ok || !semver.IsValid(v) || semver.Build(v) != "" {
			return "", false
		}
		if mj := semver.Major(v); !(mj == major || (major == "v0" || major == "v1") && (mj == "v0" || mj == "v1")) {
			return "", false
		}
		if major != "v0" && major != "v1" && strings.HasSuffix(v, "+incompatible") {
			return "", false
		}
		return v, true
	}
	best := func(tags []string) string {
		b := ""
		for _, t := range tags {
			if v, ok := valid(t); ok && (b == "" || semver.Compare(v, b) > 0) {
				b = v
			}
		}
		return b
	}
	dirty := ""
	if s.modified {
		dirty = "+dirty"
	}
	if v := best(git("tag", "--points-at", "HEAD")); v != "" {
		return v + dirty
	}
	stamp := s.time.Format("20060102150405")
	rev := s.rev
	if len(rev) > 12 {
		rev = rev[:12]
	}
	base := best(git("tag", "--merged", "HEAD"))
	var v string
	switch {
	case base == "":
		mj := major
		if mj == "v1" {
			mj = "v0"
		}
		v = mj + ".0.0-" + stamp + "-" + rev
	case semver.Prerelease(base) != "":
		v = base + ".0." + stamp + "-" + rev
	default:
		mm := strings.TrimPrefix(semver.Canonical(base), "v")
		parts := strings.SplitN(mm, ".", 3)
		patch, _ := strconv.Atoi(parts[2])
		v = fmt.Sprintf("v%s.%s.%d-0.%s-%s", parts[0], parts[1], patch+1, stamp, rev)
	}
	return v + dirty
}

// rootModPath is the module path in dir/go.mod, "" without one.
func rootModPath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	return modfile.ModulePath(data)
}

// trimMajor drops a /vN major version suffix (N >= 2) from a module path.
func trimMajor(path string) string {
	if mj := majorOf(path); mj != "v1" {
		return strings.TrimSuffix(path, "/"+mj)
	}
	return path
}

// majorOf is the major version a module path allows: vN for a /vN suffix
// (N >= 2), v1 otherwise.
func majorOf(path string) string {
	i := strings.LastIndex(path, "/v")
	if i >= 0 {
		if n, err := strconv.Atoi(path[i+2:]); err == nil && n >= 2 {
			return "v" + path[i+2:]
		}
	}
	return "v1"
}

// buildInfoObject assembles the build info into an object for the link:
// the .go.buildinfo section as the go linker lays it out (cmd/link,
// buildinfo: the 14-byte magic, the pointer size, flags 2 for strings
// inline, padding to 32 bytes, then the Go version and the build info
// text, each after its length as a uvarint, padded to 16 bytes), so go
// version -m reads it, and the strong definitions of marmot_modinfo and
// marmot_modinfo_len that rt_modinfo reads (runtime/core/src/modinfo.S
// has the weak defaults). The section is kept by the linker even in a
// program that never calls debug.ReadBuildInfo.
func (b *build) buildInfoObject(goVer, text string) (string, error) {
	data := []byte("\xff Go buildinf:")
	data = append(data, 8, 2)
	data = append(data, make([]byte, 32-len(data))...)
	data = binary.AppendUvarint(data, uint64(len(goVer)))
	data = append(data, goVer...)
	data = binary.AppendUvarint(data, uint64(len(text)))
	off := len(data)
	data = append(data, text...)
	for len(data)%16 != 0 {
		data = append(data, 0)
	}
	sym := func(s string) string {
		if b.goos == "darwin" {
			return "_" + s
		}
		return s
	}
	// Hidden, as the weak defaults in runtime/core/src/modinfo.S: the
	// symbols are for rt_modinfo only, not exported from shared builds.
	hidden := ".hidden"
	if b.goos == "darwin" {
		hidden = ".private_extern"
	}
	var s strings.Builder
	if b.goos == "darwin" {
		s.WriteString("\t.section __DATA,__go_buildinfo\n")
	} else {
		s.WriteString("\t.section .go.buildinfo,\"awR\",@progbits\n")
	}
	s.WriteString("\t.p2align 4\n")
	fmt.Fprintf(&s, "marmot_buildinfo_start:\n")
	writeBytes(&s, data[:off])
	fmt.Fprintf(&s, "\t.globl %[1]s\n\t%[2]s %[1]s\n%[1]s:\n", sym("marmot_modinfo"), hidden)
	if b.goos == "darwin" {
		fmt.Fprintf(&s, "\t.no_dead_strip %s\n", sym("marmot_modinfo"))
	}
	writeBytes(&s, data[off:])
	fmt.Fprintf(&s, "\t.p2align 3\n\t.globl %[1]s\n\t%[3]s %[1]s\n%[1]s:\n\t.quad %[2]d\n", sym("marmot_modinfo_len"), len(text), hidden)
	if b.goos != "darwin" {
		s.WriteString("\t.section .note.GNU-stack,\"\",@progbits\n")
	}
	src := filepath.Join(b.work, "buildinfo.s")
	obj := filepath.Join(b.work, "buildinfo.o")
	if err := os.WriteFile(src, []byte(s.String()), 0o644); err != nil {
		return "", err
	}
	args := []string{"-c", src, "-o", obj}
	if b.cross() || b.cfg.Static {
		args = append([]string{"--target=" + b.cTriple()}, args...)
	}
	if err := b.run(llvmTool("clang"), args...); err != nil {
		return "", fmt.Errorf("build info object: %w", err)
	}
	return obj, nil
}

// writeBytes writes data as .byte lines.
func writeBytes(s *strings.Builder, data []byte) {
	for i := 0; i < len(data); i += 32 {
		s.WriteString("\t.byte ")
		for j := i; j < min(i+32, len(data)); j++ {
			if j > i {
				s.WriteByte(',')
			}
			s.WriteString(strconv.Itoa(int(data[j])))
		}
		s.WriteByte('\n')
	}
}
