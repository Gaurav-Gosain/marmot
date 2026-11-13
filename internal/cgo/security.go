// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE-go file.

// Checking of #cgo compiler and linker flags, adapted from cmd/go
// (cmd/go/internal/work/security.go): a package may only pass flags on
// these lists to clang and the linker, as with go build, since a flag such
// as -fplugin= or -Wl,--plugin would run code at build time.
// $CGO_CFLAGS_ALLOW, $CGO_LDFLAGS_DISALLOW and the rest override the lists.
// cmd/go's further checks of the files that linker flags name (archives,
// objects, linker scripts) are not done here.

package cgo

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var re = regexp.MustCompile

var validCompilerFlags = []*regexp.Regexp{
	re(`-D([A-Za-z_][A-Za-z0-9_]*)(=[^@\-]*)?`),
	re(`-U([A-Za-z_][A-Za-z0-9_]*)`),
	re(`-F([^@\-].*)`),
	re(`-I([^@\-].*)`),
	re(`-O`),
	re(`-O([^@\-].*)`),
	re(`-W`),
	re(`-W([^@,]+)`), // -Wall but not -Wa,-foo.
	re(`-Wa,-mbig-obj`),
	re(`-Wp,-D([A-Za-z_][A-Za-z0-9_]*)(=[^@,\-]*)?`),
	re(`-Wp,-U([A-Za-z_][A-Za-z0-9_]*)`),
	re(`-ansi`),
	re(`-f(no-)?asynchronous-unwind-tables`),
	re(`-f(no-)?blocks`),
	re(`-f(no-)builtin-[a-zA-Z0-9_]*`),
	re(`-f(no-)?common`),
	re(`-f(no-)?constant-cfstrings`),
	re(`-fdebug-prefix-map=([^@]+)=([^@]+)`),
	re(`-fdiagnostics-show-note-include-stack`),
	re(`-ffile-prefix-map=([^@]+)=([^@]+)`),
	re(`-fno-canonical-system-headers`),
	re(`-f(no-)?eliminate-unused-debug-types`),
	re(`-f(no-)?exceptions`),
	re(`-f(no-)?fast-math`),
	re(`-f(no-)?inline-functions`),
	re(`-finput-charset=([^@\-].*)`),
	re(`-f(no-)?fat-lto-objects`),
	re(`-f(no-)?keep-inline-dllexport`),
	re(`-f(no-)?lto`),
	re(`-fmacro-backtrace-limit=(.+)`),
	re(`-fmessage-length=(.+)`),
	re(`-f(no-)?modules`),
	re(`-f(no-)?objc-arc`),
	re(`-f(no-)?objc-nonfragile-abi`),
	re(`-f(no-)?objc-legacy-dispatch`),
	re(`-f(no-)?omit-frame-pointer`),
	re(`-f(no-)?openmp(-simd)?`),
	re(`-f(no-)?permissive`),
	re(`-f(no-)?(pic|PIC|pie|PIE)`),
	re(`-f(no-)?plt`),
	re(`-f(no-)?rtti`),
	re(`-f(no-)?split-stack`),
	re(`-f(no-)?stack-(.+)`),
	re(`-f(no-)?strict-aliasing`),
	re(`-f(un)signed-char`),
	re(`-f(no-)?use-linker-plugin`), // safe if -B is not used; we don't permit -B
	re(`-f(no-)?visibility-inlines-hidden`),
	re(`-fsanitize=([^@\-].*)`),
	re(`-fsanitize-undefined-strip-path-components=(-)?[0-9]+`),
	re(`-ftemplate-depth-(.+)`),
	re(`-ftls-model=(global-dynamic|local-dynamic|initial-exec|local-exec)`),
	re(`-fvisibility=(.+)`),
	re(`-g([^@\-].*)?`),
	re(`-m32`),
	re(`-m64`),
	re(`-m(abi|arch|cpu|fpu|simd|tls-dialect|tune)=([^@\-].*)`),
	re(`-m(no-)?v?aes`),
	re(`-marm`),
	re(`-mcmodel=[0-9a-z-]+`),
	re(`-mfloat-abi=([^@\-].*)`),
	re(`-m(soft|single|double)-float`),
	re(`-mfpmath=[0-9a-z,+]*`),
	re(`-m(no-)?avx[0-9a-z.]*`),
	re(`-m(no-)?ms-bitfields`),
	re(`-m(no-)?stack-(.+)`),
	re(`-mmacosx-(.+)`),
	re(`-m(no-)?relax`),
	re(`-m(no-)?strict-align`),
	re(`-m(no-)?(lsx|lasx|frecipe|div32|lam-bh|lamcas|ld-seq-sa)`),
	re(`-mios-simulator-version-min=(.+)`),
	re(`-miphoneos-version-min=(.+)`),
	re(`-mlarge-data-threshold=[0-9]+`),
	re(`-mtvos-simulator-version-min=(.+)`),
	re(`-mtvos-version-min=(.+)`),
	re(`-mwatchos-simulator-version-min=(.+)`),
	re(`-mwatchos-version-min=(.+)`),
	re(`-mnop-fun-dllimport`),
	re(`-m(no-)?sse[0-9.]*`),
	re(`-m(no-)?ssse3`),
	re(`-mthumb(-interwork)?`),
	re(`-mthreads`),
	re(`-mwindows`),
	re(`-no-canonical-prefixes`),
	re(`--param=ssp-buffer-size=[0-9]*`),
	re(`-pedantic(-errors)?`),
	re(`-pipe`),
	re(`-pthread`),
	re(`--static`),
	re(`-?-std=([^@\-].*)`),
	re(`-?-stdlib=([^@\-].*)`),
	re(`-w`),
	re(`-x(c|c-header|cpp-output|c\+\+|c\+\+-header|c\+\+-cpp-output|objective-c|objective-c-header|objective-c-cpp-output|objective-c\+\+|objective-c\+\+-header|objective-c\+\+-cpp-output|assembler|assembler-with-cpp)`),
	re(`-v`),
}

var validCompilerXLang = re(`^(c|c-header|cpp-output|c\+\+|c\+\+-header|c\+\+-cpp-output|objective-c|objective-c-header|objective-c-cpp-output|objective-c\+\+|objective-c\+\+-header|objective-c\+\+-cpp-output|assembler|assembler-with-cpp)$`)

var validCompilerFlagsWithNextArg = []string{
	"-arch",
	"-D",
	"-U",
	"-I",
	"-F",
	"-framework",
	"-include",
	"-isysroot",
	"-isystem",
	"-x",
}

var invalidCompilerFlags = []*regexp.Regexp{
	// Clang's -gen-cdb-fragment-path takes a directory argument and writes
	// compilation database fragments there; have to exclude separately because
	// -g* is allowed in general. GCC's -gnatO similarly takes a separate
	// output filename argument in the GCC driver.
	re(`-gen-.*`),
	re(`-gnat.*`),
}

var invalidLinkerFlags = []*regexp.Regexp{
	// On macOS this means the linker loads and executes the next argument.
	// Have to exclude separately because -lfoo is allowed in general.
	re(`-lto_library`),
	re(`-lazy_library`),
	re(`-lazy_framework`),
	re(`-load_hidden`),
	// GNU gold and LLD accept single-dash -library and -library-path.
	re(`-library(-path)?(=.*)?`),
	re(`-gen-.*`),
	re(`-gnat.*`),
	// In lld, --warn-backrefs-exclude takes a separate <glob> argument when not
	// joined with '='; exclude the bare form because -Wl,--(no-)?warn-([^,]+) is
	// allowed in general.
	re(`-Wl,--warn-backrefs-exclude`),
}

var validLinkerFlags = []*regexp.Regexp{
	re(`-F([^@\-].*)`),
	re(`-l([^@\-:=/\\][^:=/\\]*)`),
	re(`-L([^@\-].*)`),
	re(`-O`),
	re(`-O([^@\-].*)`),
	re(`-f(no-)?(pic|PIC|pie|PIE)`),
	re(`-f(no-)?openmp(-simd)?`),
	re(`-fsanitize=([^@\-].*)`),
	re(`-flat_namespace`),
	re(`-g([^@\-].*)?`),
	re(`-headerpad_max_install_names`),
	re(`-m(abi|arch|cpu|fpu|simd|tls-dialect|tune)=([^@\-].*)`),
	re(`-mcmodel=[0-9a-z-]+`),
	re(`-mfloat-abi=([^@\-].*)`),
	re(`-m(soft|single|double)-float`),
	re(`-m(no-)?relax`),
	re(`-m(no-)?strict-align`),
	re(`-m(no-)?(lsx|lasx|frecipe|div32|lam-bh|lamcas|ld-seq-sa)`),
	re(`-mmacosx-(.+)`),
	re(`-mios-simulator-version-min=(.+)`),
	re(`-miphoneos-version-min=(.+)`),
	re(`-mthreads`),
	re(`-mwindows`),
	re(`-(pic|PIC|pie|PIE)`),
	re(`-pthread`),
	re(`-rdynamic`),
	re(`-shared`),
	re(`-?-static([-a-z0-9+]*)`),
	re(`-?-stdlib=([^@\-].*)`),
	re(`-v`),

	// Note that any wildcards in -Wl need to exclude comma,
	// since -Wl splits its argument at commas and passes
	// them all to the linker uninterpreted. Allowing comma
	// in a wildcard would allow tunneling arbitrary additional
	// linker arguments through one of these.
	re(`-Wl,--(no-)?allow-multiple-definition`),
	re(`-Wl,--(no-)?allow-shlib-undefined`),
	re(`-Wl,--(no-)?as-needed`),
	re(`-Wl,-Bdynamic`),
	re(`-Wl,-berok`),
	re(`-Wl,-Bstatic`),
	re(`-Wl,-Bsymbolic-functions`),
	re(`-Wl,-O[0-9]+`),
	re(`-Wl,-d[ny]`),
	re(`-Wl,--disable-new-dtags`),
	re(`-Wl,-e[=,][a-zA-Z0-9]+`),
	re(`-Wl,--enable-new-dtags`),
	re(`-Wl,--end-group`),
	re(`-Wl,--(no-)?export-dynamic`),
	re(`-Wl,-E`),
	re(`-Wl,-framework,[^,@\-][^,]*`),
	re(`-Wl,--hash-style=(sysv|gnu|both)`),
	re(`-Wl,-headerpad_max_install_names`),
	re(`-Wl,--no-undefined`),
	re(`-Wl,--pop-state`),
	re(`-Wl,--push-state`),
	re(`-Wl,-R[,=]?([^@\-,=][^,@]*$)`),
	re(`-Wl,--just-symbols[=,]([^,@\-][^,@]*)`),
	re(`-Wl,-rpath(-link)?[=,]([^,@\-][^,]*)`),
	re(`-Wl,-s`),
	re(`-Wl,-search_paths_first`),
	re(`-Wl,-sectcreate,([^,@\-][^,]*),([^,@\-][^,]*),([^,@\-][^,]*)`),
	re(`-Wl,--start-group`),
	re(`-Wl,-?-static`),
	re(`-Wl,-?-subsystem,(native|windows|console|posix|xbox)`),
	re(`-Wl,-syslibroot[=,]([^,@\-][^,]*)`),
	re(`-Wl,-undefined[=,]([^,@\-][^,]*)`),
	re(`-Wl,-?-unresolved-symbols=[^,]+`),
	re(`-Wl,--(no-)?warn-([^,]+)`),
	re(`-Wl,-?-wrap[=,][^,@\-][^,]*`),
	re(`-Wl(,-z,(relro|now|(no)?execstack))+`),

	re(`[a-zA-Z0-9_/].*\.(a|o|obj|dll|dylib|lib|so|tbd)`), // direct linker inputs: x.o or libfoo.so (but not -foo.o or @foo.o)
	re(`\./.*\.(a|o|obj|dll|dylib|lib|so|tbd)`),
}

var validLinkerFlagsWithNextArg = []string{
	"-arch",
	"-F",
	"-l",
	"-L",
	"-framework",
	"-isysroot",
	"-Wl,-framework",
	"-Wl,-rpath",
	"-Wl,-R",
	"-Wl,--just-symbols",
	"-Wl,-undefined",
}

// recombinedLinkerFlagsWithNextArg lists flags in validLinkerFlagsWithNextArg
// that GCC and Clang recombine with their following argument (e.g., "-l" "foo"
// becomes "-lfoo"), which must also be checked against invalidLinkerFlags.
var recombinedLinkerFlagsWithNextArg = []string{
	"-l",
}

var validPkgConfigFlags = []*regexp.Regexp{
	re(`--atleast-pkgconfig-version=\d+\.\d+\.\d+`),
	re(`--atleast-version=\d+\.\d+\.\d+`),
	re(`--cflags-only-I`),
	re(`--cflags`),
	re(`--define-prefix`),
	re(`--define-variable=[A-Za-z_][A-Za-z0-9_]*=[^@\-=$][^@\-$]*`),
	re(`--digraph`),
	re(`--dont-define-prefix`),
	re(`--dont-relocate-paths`),
	re(`--dump-personality`),
	re(`--env-only`),
	re(`--errors-to-stdout`),
	re(`--exact-version=\d+\.\d+\.\d+`),
	re(`--exists`),
	re(`--fragment-filter=[A-Za-z_][a-zA-Z0-9_]*`),
	re(`--ignore-conflicts`),
	re(`--internal-cflags`),
	re(`--keep-system-cflags`),
	re(`--keep-system-libs`),
	re(`--libs-only-l`),
	re(`--libs-only-L`),
	re(`--libs`),
	re(`--list-all`),
	re(`--list-package-names`),
	re(`--max-version=\d+\.\d+\.\d+`),
	re(`--maximum-traverse-depth=[0-9]+`),
	re(`--modversion`),
	re(`--msvc-syntax`),
	re(`--no-cache`),
	re(`--no-provides`),
	re(`--no-uninstalled`),
	re(`--path`),
	re(`--prefix-variable=[A-Za-z_][a-zA-Z0-9_]*`),
	re(`--print-errors`),
	re(`--print-provides`),
	re(`--print-requires-private`),
	re(`--print-requires`),
	re(`--print-variables`),
	re(`--pure`),
	re(`--shared`),
	re(`--short-errors`),
	re(`--silence-errors`),
	re(`--simulate`),
	re(`--static`),
	re(`--uninstalled`),
	re(`--validate`),
	re(`--variable=[A-Za-z_][a-zA-Z0-9_]*`),
}

// hasGCCSpecFlags reports whether list contains linker flags that cause GCC's
// built-in *link_command spec to evaluate %:include(libgomp.spec) (-fopenmp or
// -fopenacc) or %:include(libsanitizer.spec) (-fsanitize=* combined with
// -static or -static-lib*).

func checkCompilerFlags(name, source string, list []string) error {
	return checkFlags(name, source, list, invalidCompilerFlags, validCompilerFlags, nil, validCompilerFlagsWithNextArg, true)
}

func checkLinkerFlags(name, source string, list []string) error {
	return checkFlags(name, source, list, invalidLinkerFlags, validLinkerFlags, recombinedLinkerFlagsWithNextArg, validLinkerFlagsWithNextArg, true)
}

func checkPkgConfigFlags(name, source string, list []string) error {
	return checkFlags(name, source, list, nil, validPkgConfigFlags, nil, nil, false)
}

// safeArg is cmd/go's safeArg: name does not look like an option.
func safeArg(name string) bool {
	if name == "" {
		return false
	}
	c := name[0]
	return '0' <= c && c <= '9' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || c == '.' || c == '_' || c == '/' || c >= utf8.RuneSelf
}

func validPkgConfigName(pkg string) bool {
	if !safeArg(pkg) || !utf8.ValidString(pkg) {
		return false
	}
	if strings.ContainsAny(pkg, `/\:`) || strings.Contains(pkg, "..") {
		return false
	}
	fields := strings.FieldsFunc(pkg, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		lower := strings.ToLower(field)
		if strings.HasSuffix(lower, ".pc") {
			return false
		}
		for i := range len(lower) {
			if strings.IndexByte("<>=!", lower[i]) >= 0 && strings.HasSuffix(lower[:i], ".pc") {
				return false
			}
		}
	}
	return true
}

func checkFlags(name, source string, list []string, invalid, valid []*regexp.Regexp, recombineNext, validNext []string, checkOverrides bool) error {
	// Let users override rules with $CGO_CFLAGS_ALLOW, $CGO_CFLAGS_DISALLOW, etc.
	var (
		allow    *regexp.Regexp
		disallow *regexp.Regexp
	)
	if checkOverrides {
		if env := os.Getenv("CGO_" + name + "_ALLOW"); env != "" {
			r, err := regexp.Compile(env)
			if err != nil {
				return fmt.Errorf("parsing $CGO_%s_ALLOW: %v", name, err)
			}
			allow = r
		}
		if env := os.Getenv("CGO_" + name + "_DISALLOW"); env != "" {
			r, err := regexp.Compile(env)
			if err != nil {
				return fmt.Errorf("parsing $CGO_%s_DISALLOW: %v", name, err)
			}
			disallow = r
		}
	}

Args:
	for i := 0; i < len(list); i++ {
		arg := list[i]
		if disallow != nil && disallow.FindString(arg) == arg {
			goto Bad
		}
		if allow != nil && allow.FindString(arg) == arg {
			continue Args
		}
		for _, re := range invalid {
			if re.FindString(arg) == arg { // must be complete match
				goto Bad
			}
		}
		for _, re := range valid {
			if match := re.FindString(arg); match == arg { // must be complete match
				continue Args
			} else if strings.HasPrefix(arg, "-Wl,--push-state,") {
				// Examples for --push-state are written
				//     -Wl,--push-state,--as-needed
				// Support other commands in the same -Wl arg.
				args := strings.Split(arg, ",")
				for _, a := range args[1:] {
					a = "-Wl," + a
					var found bool
					for _, re := range valid {
						if re.FindString(a) == a {
							found = true
							break
						}
					}
					if !found {
						goto Bad
					}
					for _, re := range invalid {
						if re.FindString(a) == a {
							goto Bad
						}
					}
				}
				continue Args
			}
		}
		if slices.Contains(recombineNext, arg) && i+1 < len(list) {
			combined := arg + list[i+1]
			if disallow != nil && disallow.FindString(combined) == combined {
				goto Bad
			}
			if allow == nil || allow.FindString(combined) != combined {
				for _, re := range invalid {
					if re.FindString(combined) == combined {
						goto Bad
					}
				}
			}
		}
		for _, x := range validNext {
			if arg == x {
				if arg == "-x" {
					if i+1 < len(list) && validCompilerXLang.MatchString(list[i+1]) {
						i++
						continue Args
					}
					if i+1 < len(list) {
						return fmt.Errorf("invalid flag in %s: %s %s (see https://go.dev/s/invalidflag)", source, arg, list[i+1])
					}
					return fmt.Errorf("invalid flag in %s: %s without argument (see https://go.dev/s/invalidflag)", source, arg)
				}
				if strings.HasPrefix(arg, "-Wl,") {
					// Permit -Wl,-framework -Wl,name.
					if i+1 < len(list) &&
						strings.HasPrefix(list[i+1], "-Wl,") &&
						safeArg(list[i+1][4:]) &&
						!strings.Contains(list[i+1][4:], ",") {
						i++
						continue Args
					}
					// Permit -Wl,-R /foo and -Wl,-R foo.
					if arg == "-Wl,-R" && i+1 < len(list) && safeArg(list[i+1]) {
						i++
						continue Args
					}
					if i+1 < len(list) {
						return fmt.Errorf("invalid flag in %s: %s %s (see https://go.dev/s/invalidflag)", source, arg, list[i+1])
					}
					return fmt.Errorf("invalid flag in %s: %s without argument (see https://go.dev/s/invalidflag)", source, arg)
				}
				if arg == "-l" {
					if i+1 < len(list) && safeArg(list[i+1]) && !strings.ContainsAny(list[i+1], `:=/\`) {
						i++
						continue Args
					}
					if i+1 < len(list) {
						return fmt.Errorf("invalid flag in %s: %s %s (see https://go.dev/s/invalidflag)", source, arg, list[i+1])
					}
					return fmt.Errorf("invalid flag in %s: %s without argument (see https://go.dev/s/invalidflag)", source, arg)
				}
				if i+1 < len(list) && safeArg(list[i+1]) {
					i++
					continue Args
				}

				// Permit -I= /path, -I $SYSROOT.
				if i+1 < len(list) && arg == "-I" {
					if (strings.HasPrefix(list[i+1], "=") || strings.HasPrefix(list[i+1], "$SYSROOT")) &&
						safeArg(list[i+1][1:]) {
						i++
						continue Args
					}
				}

				if i+1 < len(list) {
					return fmt.Errorf("invalid flag in %s: %s %s (see https://go.dev/s/invalidflag)", source, arg, list[i+1])
				}
				return fmt.Errorf("invalid flag in %s: %s without argument (see https://go.dev/s/invalidflag)", source, arg)
			}
		}
	Bad:
		return fmt.Errorf("invalid flag in %s: %s (see https://go.dev/s/invalidflag)", source, arg)
	}
	return nil
}
