package verify

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/marmot/internal/verify/smt"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

// diffInput is one input of a differential run.
type diffInput struct {
	args []string // Go source of the arguments
	env  *smt.Env
}

// randInput draws an input for the encoded parameters: boundary values
// often, random ones otherwise; byte slices with their capacity.
func randInput(r *rand.Rand, ps []encParam) diffInput {
	in := diffInput{env: &smt.Env{BV: map[string]uint64{}, Arr: map[string]map[uint64]byte{}}}
	for _, p := range ps {
		switch p.Kind {
		case "bool":
			v := r.IntN(2)
			in.env.BV[p.Name] = uint64(v)
			in.args = append(in.args, strconv.FormatBool(v == 1))
		case "int", "uint":
			var u uint64
			switch r.IntN(4) {
			case 0:
				u = uint64(int64(r.IntN(9) - 4))
			case 1:
				edges := []int64{math.MinInt64, math.MaxInt64, math.MinInt32, math.MaxInt32, 127, 128, -128, 255, 256, 65535, 63, 64, 65, 7, 8}
				u = uint64(edges[r.IntN(len(edges))])
			default:
				u = r.Uint64() >> uint(r.IntN(64))
			}
			u &= ^uint64(0) >> uint(64-p.Bits)
			in.env.BV[p.Name] = u
			// The literal of the value in the parameter's type.
			if p.Kind == "int" {
				s := uint(64 - p.Bits)
				in.args = append(in.args, fmt.Sprintf("%d", int64(u<<s)>>s))
			} else {
				in.args = append(in.args, fmt.Sprintf("%d", u))
			}
		case "bytes", "string":
			n := r.IntN(6)
			c := n
			if p.Kind == "bytes" {
				c += r.IntN(3)
			}
			data := make([]byte, c)
			for i := range data {
				data[i] = byte(r.IntN(256))
			}
			arr := map[uint64]byte{}
			for i, b := range data {
				arr[uint64(i)] = b
			}
			in.env.Arr[p.Name+"_arr"] = arr
			in.env.BV[p.Name+"_len"] = uint64(n)
			in.env.BV[p.Name+"_cap"] = uint64(c)
			if p.Kind == "string" {
				in.args = append(in.args, strconv.Quote(string(data[:n])))
			} else {
				in.args = append(in.args, fmt.Sprintf("mk(%d, %s)", n, strconv.Quote(string(data))))
			}
		}
	}
	return in
}

// TestEncoderAgainstGC runs every encodable function of testdata/src/diff
// under gc on random inputs and checks that the formula predicts the
// same outcome: a panic exactly when some violation condition holds, and
// otherwise the same first result.
func TestEncoderAgainstGC(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a test binary")
	}
	var funcs []*ssa.Function
	a := &analysis.Analyzer{
		Name: "enc", Doc: "encode", Requires: []*analysis.Analyzer{buildssa.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			funcs = pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA).SrcFuncs
			return nil, nil
		},
	}
	analysistest.Run(quiet{}, analysistest.TestData(), a, "diff")
	r := rand.New(rand.NewPCG(7, 11))
	type job struct {
		name   string
		enc    *encoding
		inputs []diffInput
	}
	var jobs []job
	var src strings.Builder
	src.WriteString("package diff\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\nfunc mk(n int, data string) []byte { b := make([]byte, len(data)); copy(b, data); return b[:n] }\n\nfunc try(id string, f func() uint64) {\n\tdefer func() {\n\t\tif recover() != nil {\n\t\t\tfmt.Println(id, \"P\")\n\t\t}\n\t}()\n\tfmt.Println(id, f())\n}\n\nfunc TestDiffRun(t *testing.T) {\n")
	for _, fn := range funcs {
		enc, err := encodeFunc(fn)
		if err != nil {
			t.Logf("%s: %v", fn.Name(), err)
			continue
		}
		if enc.Result == nil {
			continue
		}
		j := job{name: fn.Name(), enc: enc}
		for k := 0; k < 400; k++ {
			in := randInput(r, enc.Params)
			j.inputs = append(j.inputs, in)
			fmt.Fprintf(&src, "\ttry(%q, func() uint64 { return uint64(%s(%s)) })\n", fmt.Sprintf("%s/%d", fn.Name(), k), fn.Name(), strings.Join(in.args, ", "))
		}
		jobs = append(jobs, j)
	}
	src.WriteString("}\n")
	if len(jobs) < 15 {
		t.Fatalf("only %d functions encoded", len(jobs))
	}
	dir, _ := filepath.Abs(filepath.Join("testdata", "src", "diff"))
	tmp := t.TempDir()
	gen := filepath.Join(tmp, "zz_diff_test.go")
	if err := os.WriteFile(gen, []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	ov, _ := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(dir, "zz_diff_test.go"): gen}})
	ovf := filepath.Join(tmp, "ov.json")
	if err := os.WriteFile(ovf, ov, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "TestDiffRun", "-v", "-overlay", ovf, ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil && !bytes.Contains(out, []byte("PASS")) {
		t.Fatalf("go test: %v\n%s", err, firstLines(string(out), 20))
	}
	got := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && strings.Contains(f[0], "/") {
			got[f[0]] = f[1]
		}
	}
	bad := 0
	for _, j := range jobs {
		for k, in := range j.inputs {
			id := fmt.Sprintf("%s/%d", j.name, k)
			panics := false
			for _, v := range j.enc.Viol {
				if smt.Eval(v, in.env) != 0 {
					panics = true
				}
			}
			want := "P"
			if !panics {
				res := smt.Eval(j.enc.Result, in.env)
				w := j.enc.Result.W
				// The Go side prints uint64(result): sign extension of a
				// signed result is what uint64() does.
				if w < 64 && signedResult(j.name) {
					s := uint(64 - w)
					res = uint64(int64(res<<s) >> s)
				}
				want = strconv.FormatUint(res, 10)
			}
			if got[id] != want {
				bad++
				if bad <= 15 {
					t.Errorf("%s(%s): gc %s, formula %s", j.name, strings.Join(in.args, ", "), got[id], want)
				}
			}
		}
	}
	t.Logf("%d functions, %d inputs, %d disagreements", len(jobs), len(jobs)*400, bad)
}

// signedResult: the testdata functions whose result type is signed.
func signedResult(name string) bool {
	switch name {
	case "Sub16", "UDiv", "Shl", "ShrU", "ConvU", "Max", "At", "StrAt", "Last":
		return false
	}
	return true
}
