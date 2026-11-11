package runtime

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGo runs a Go program with the host toolchain and returns its stderr
// and exit code.
func runGo(t *testing.T, src string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "prog")
	build := exec.Command("go", "build", "-o", bin, file)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stderr.String(), code
}

var floatCases = []float64{
	0, 1, -1, 0.5, 1.5, 3.14159265358979, 1e10, 1e-10, 123456789, 1e100, 1e-300,
	1e308, 5e-324, 9.9999999, 0.1, 0.2, 0.3, 2.5e-5, 999999.95, math.MaxFloat64,
	-0.000123, 100, 1e21, 7, 6.02214076e23,
}

var floatSrc = []string{
	"0", "1", "-1", "0.5", "1.5", "3.14159265358979", "1e10", "1e-10", "123456789", "1e100", "1e-300",
	"1e308", "5e-324", "9.9999999", "0.1", "0.2", "0.3", "2.5e-5", "999999.95", "math.MaxFloat64",
	"-0.000123", "100", "1e21", "7", "6.02214076e23",
}

var intCases = []int64{0, 1, -1, 42, -42, 1 << 62, math.MaxInt64, math.MinInt64, 1000000007}

var intSrc = []string{"0", "1", "-1", "42", "-42", "1 << 62", "math.MaxInt64", "math.MinInt64", "1000000007"}

func TestPrintMatchesGo(t *testing.T) {
	var src strings.Builder
	src.WriteString("package main\n\nimport \"math\"\n\nvar _ = math.Pi\n\nfunc main() {\n")
	for _, s := range floatSrc {
		src.WriteString("\tprintln(float64(" + s + "))\n")
	}
	src.WriteString("\tvar z float64\n\tprintln(-z, z, 1/z, -1/z, z/z)\n")
	for _, s := range intSrc {
		src.WriteString("\tprintln(int64(" + s + "))\n")
	}
	src.WriteString("\tprintln(uint64(math.MaxUint64), uint8(200), true, false, \"str\", \"\")\n")
	src.WriteString("\tprintln(complex(1.5, -2))\n")
	src.WriteString("\tprint(1, 2, \"x\", \"\\n\")\n")
	src.WriteString("}\n")
	want, code := runGo(t, src.String())
	if code != 0 {
		t.Fatalf("host program failed: %s", want)
	}

	takeOutput()
	println2 := func(fs ...func()) {
		printlock()
		for i, f := range fs {
			if i > 0 {
				printsp()
			}
			f()
		}
		printnl()
		printunlock()
	}
	for _, f := range floatCases {
		f := f
		println2(func() { printfloat(f) })
	}
	var z float64
	println2(func() { printfloat(-z) }, func() { printfloat(z) }, func() { printfloat(1 / z) },
		func() { printfloat(-1 / z) }, func() { printfloat(z / z) })
	for _, v := range intCases {
		v := v
		println2(func() { printint(v) })
	}
	println2(func() { printuint(math.MaxUint64) }, func() { printuint(200) }, func() { printbool(true) },
		func() { printbool(false) }, func() { printstring("str") }, func() { printstring("") })
	println2(func() { printcomplex(complex(1.5, -2)) })
	printlock()
	printint(1)
	printint(2)
	printstring("x")
	printstring("\n")
	printunlock()
	got := takeOutput()
	if got != want {
		t.Errorf("print mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrintHex(t *testing.T) {
	takeOutput()
	printhex(0)
	printstring(" ")
	printhex(0xdeadbeef)
	printstring(" ")
	printslice(nil, 3, 4)
	if got := takeOutput(); got != "0x0 0xdeadbeef [3/4]0x0" {
		t.Errorf("got %q", got)
	}
}

func TestPrintLockBuffers(t *testing.T) {
	takeOutput()
	printlock()
	printstring("a")
	printlock()
	printstring("b")
	printunlock()
	if out.Len() != 0 {
		t.Errorf("output written while print lock held")
	}
	printunlock()
	if got := takeOutput(); got != "ab" {
		t.Errorf("got %q", got)
	}
	// Large writes go through even when they exceed the buffer.
	big := strings.Repeat("x", 2000)
	printlock()
	printstring("y")
	printstring(big)
	printunlock()
	if got := takeOutput(); got != "y"+big {
		t.Errorf("large write lost data: %d bytes", len(got))
	}
}

func TestPrintIndented(t *testing.T) {
	takeOutput()
	printindented("a\nb\nc")
	if got := takeOutput(); got != "a\n\tb\n\tc" {
		t.Errorf("got %q", got)
	}
}
