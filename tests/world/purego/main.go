// purego calls C functions without cgo. This test compiles a small C
// library with cc, loads it with purego.Dlopen and calls one function of
// each signature shape purego supports through RegisterLibFunc: integers
// of every size and sign, bools, floats, more arguments than registers
// (integers and floats on the stack), pointers, strings, structs by value
// (small, all-float, mixed, large) as arguments and results, and C calling
// back into Go through NewCallback (integer results: purego callbacks
// cannot return floats). It also calls
// SyscallN and libc's qsort with a Go comparison function.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

const csrc = `
#include <stdint.h>
#include <stdbool.h>
#include <string.h>
#include <ctype.h>

int8_t t_i8(int8_t a, int8_t b) { return (int8_t)(a + b); }
uint8_t t_u8(uint8_t a) { return (uint8_t)(a * 3); }
int16_t t_i16(int16_t a, int16_t b) { return (int16_t)(a - b); }
uint16_t t_u16(uint16_t a) { return (uint16_t)(a << 3); }
int32_t t_i32(int32_t a, int32_t b) { return a * b; }
uint32_t t_u32(uint32_t a) { return ~a; }
int64_t t_i64(int64_t a, int64_t b) { return a * b - 1; }
uint64_t t_u64(uint64_t a) { return a ^ 0xF0F0F0F0F0F0F0F0ull; }
intptr_t t_iptr(intptr_t a) { return -a; }
bool t_bool(bool a, bool b) { return a && !b; }
float t_f32(float a, float b) { return a * b + 0.5f; }
double t_f64(double a, double b) { return a / b; }
double t_mixed(int8_t a, double b, int32_t c, float d, int64_t e, uint16_t f) {
  return a + b * 2 + c * 3 + d * 4 + e * 5 + f * 6;
}
int64_t t_many_ints(int64_t a1, int64_t a2, int64_t a3, int64_t a4, int64_t a5, int64_t a6,
                    int64_t a7, int64_t a8, int64_t a9, int64_t a10, int64_t a11, int64_t a12) {
  return a1 + 2 * a2 + 3 * a3 + 4 * a4 + 5 * a5 + 6 * a6 + 7 * a7 + 8 * a8 + 9 * a9 +
         10 * a10 + 11 * a11 + 12 * a12;
}
double t_many_floats(double a1, double a2, double a3, double a4, double a5, double a6,
                     double a7, double a8, double a9, double a10, float a11) {
  return a1 + 2 * a2 + 3 * a3 + 4 * a4 + 5 * a5 + 6 * a6 + 7 * a7 + 8 * a8 + 9 * a9 +
         10 * a10 + 11 * a11;
}
// Small integers after the registers are full: packed by size on darwin/arm64.
int64_t t_stack_small(int64_t a1, int64_t a2, int64_t a3, int64_t a4, int64_t a5, int64_t a6,
                      int64_t a7, int64_t a8, int32_t s1, int8_t s2, int16_t s3, int64_t s4) {
  return a1 + a2 + a3 + a4 + a5 + a6 + a7 + a8 + s1 * 100 + s2 * 1000 + s3 * 10000 + s4;
}
static char up[64];
const char *t_upper(const char *s) {
  size_t i = 0;
  for (; s[i] && i < sizeof up - 1; i++) up[i] = (char)toupper((unsigned char)s[i]);
  up[i] = 0;
  return up;
}
size_t t_strlen(const char *s) { return strlen(s); }
void *t_ptr(void *p, intptr_t off) { return (char *)p + off; }
void t_fill(int32_t *out, int32_t n) { for (int32_t i = 0; i < n; i++) out[i] = i * i; }

struct small { int32_t a; int32_t b; };
struct hfa { float x, y, z, w; };
struct dbl2 { double a, b; };
struct mix { int64_t a; double b; };
struct big { int64_t a, b, c, d; };
struct pad { char c; int64_t x; int16_t s; };

struct small t_small(int32_t a, int32_t b) { struct small s = {a + 1, b * 2}; return s; }
int32_t t_small_sum(struct small s) { return s.a * 10 + s.b; }
struct hfa t_hfa(float v) { struct hfa h = {v, v * 2, v * 3, v * 4}; return h; }
float t_hfa_sum(struct hfa h) { return h.x + h.y * 10 + h.z * 100 + h.w * 1000; }
struct dbl2 t_dbl2(double a) { struct dbl2 d = {a, -a}; return d; }
struct mix t_mix(int64_t a, double b) { struct mix m = {a * 2, b / 2}; return m; }
double t_mix_sum(struct mix m) { return (double)m.a + m.b; }
struct big t_big(int64_t s) { struct big b = {s, s + 1, s + 2, s + 3}; return b; }
int64_t t_big_sum(struct big b) { return b.a + b.b * 10 + b.c * 100 + b.d * 1000; }
struct pad t_pad(char c, int64_t x, int16_t s) { struct pad p = {c, x, s}; return p; }

int64_t t_cb_int(int64_t (*f)(int64_t, int64_t), int64_t a, int64_t b) { return f(a, b) + 1; }
int64_t t_cb_float(int64_t (*f)(double, float, int32_t), double a) { return f(a, 1.5f, 7) * 2; }
int64_t t_cb_many(int64_t (*f)(int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t,
                               int64_t, int64_t, int64_t)) {
  return f(1, 2, 3, 4, 5, 6, 7, 8, 9, 10);
}
int64_t t_cb_fmany(int64_t (*f)(double, double, double, double, double, double, double, double,
                                double, double)) {
  return f(0.5, 1, 1.5, 2, 2.5, 3, 3.5, 4, 4.5, 5);
}
int32_t t_cb_small(struct small (*f)(struct small), int32_t a) {
  struct small s = {a, a + 1};
  struct small r = f(s);
  return r.a * 100 + r.b;
}
int32_t t_cb_str(int32_t (*f)(const char *), const char *s) { return f(s) * 2; }
bool t_cb_bool(bool (*f)(bool, int8_t), bool a) { return !f(a, -3); }
`

type small struct{ A, B int32 }
type hfa struct{ X, Y, Z, W float32 }
type dbl2 struct{ A, B float64 }
type mix struct {
	A int64
	B float64
}
type big struct{ A, B, C, D int64 }
type pad struct {
	C byte
	X int64
	S int16
}

// lib holds the C functions, registered by field.
var lib struct {
	i8          func(int8, int8) int8
	u8          func(uint8) uint8
	i16         func(int16, int16) int16
	u16         func(uint16) uint16
	i32         func(int32, int32) int32
	u32         func(uint32) uint32
	i64         func(int64, int64) int64
	u64         func(uint64) uint64
	iptr        func(int) int
	boolean     func(bool, bool) bool
	f32         func(float32, float32) float32
	f64         func(float64, float64) float64
	mixed       func(int8, float64, int32, float32, int64, uint16) float64
	manyInts    func(a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12 int64) int64
	manyFloats  func(a1, a2, a3, a4, a5, a6, a7, a8, a9, a10 float64, a11 float32) float64
	stackSmall  func(a1, a2, a3, a4, a5, a6, a7, a8 int64, s1 int32, s2 int8, s3 int16, s4 int64) int64
	upper       func(string) string
	strlen      func(string) uintptr
	ptr         func(unsafe.Pointer, int) unsafe.Pointer
	fill        func(*int32, int32)
	small       func(int32, int32) small
	smallSum    func(small) int32
	hfa         func(float32) hfa
	hfaSum      func(hfa) float32
	dbl2        func(float64) dbl2
	mix         func(int64, float64) mix
	mixSum      func(mix) float64
	big         func(int64) big
	bigSum      func(big) int64
	pad         func(byte, int64, int16) pad
	cbInt       func(uintptr, int64, int64) int64
	cbFloat     func(uintptr, float64) int64
	cbMany      func(uintptr) int64
	cbFmany     func(uintptr) int64
	cbSmall     func(uintptr, int32) int32
	cbStr       func(uintptr, string) int32
	cbBool      func(func(bool, int8) bool, bool) bool
}

func buildLib() string {
	dir, err := os.MkdirTemp("", "purego-world-")
	if err != nil {
		panic(err)
	}
	src := filepath.Join(dir, "t.c")
	if err := os.WriteFile(src, []byte(csrc), 0o644); err != nil {
		panic(err)
	}
	so := filepath.Join(dir, "libt.so")
	if out, err := exec.Command("cc", "-shared", "-fPIC", "-O1", "-o", so, src).CombinedOutput(); err != nil {
		panic(fmt.Sprintf("cc: %v\n%s", err, out))
	}
	return so
}

func libcPath() string {
	if runtime.GOOS == "darwin" {
		return "/usr/lib/libSystem.B.dylib"
	}
	return "libc.so.6"
}

func main() {
	so := buildLib()
	defer os.RemoveAll(filepath.Dir(so))
	h, err := purego.Dlopen(so, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&lib.i8, h, "t_i8")
	purego.RegisterLibFunc(&lib.u8, h, "t_u8")
	purego.RegisterLibFunc(&lib.i16, h, "t_i16")
	purego.RegisterLibFunc(&lib.u16, h, "t_u16")
	purego.RegisterLibFunc(&lib.i32, h, "t_i32")
	purego.RegisterLibFunc(&lib.u32, h, "t_u32")
	purego.RegisterLibFunc(&lib.i64, h, "t_i64")
	purego.RegisterLibFunc(&lib.u64, h, "t_u64")
	purego.RegisterLibFunc(&lib.iptr, h, "t_iptr")
	purego.RegisterLibFunc(&lib.boolean, h, "t_bool")
	purego.RegisterLibFunc(&lib.f32, h, "t_f32")
	purego.RegisterLibFunc(&lib.f64, h, "t_f64")
	purego.RegisterLibFunc(&lib.mixed, h, "t_mixed")
	purego.RegisterLibFunc(&lib.manyInts, h, "t_many_ints")
	purego.RegisterLibFunc(&lib.manyFloats, h, "t_many_floats")
	purego.RegisterLibFunc(&lib.stackSmall, h, "t_stack_small")
	purego.RegisterLibFunc(&lib.upper, h, "t_upper")
	purego.RegisterLibFunc(&lib.strlen, h, "t_strlen")
	purego.RegisterLibFunc(&lib.ptr, h, "t_ptr")
	purego.RegisterLibFunc(&lib.fill, h, "t_fill")
	purego.RegisterLibFunc(&lib.small, h, "t_small")
	purego.RegisterLibFunc(&lib.smallSum, h, "t_small_sum")
	purego.RegisterLibFunc(&lib.hfa, h, "t_hfa")
	purego.RegisterLibFunc(&lib.hfaSum, h, "t_hfa_sum")
	purego.RegisterLibFunc(&lib.dbl2, h, "t_dbl2")
	purego.RegisterLibFunc(&lib.mix, h, "t_mix")
	purego.RegisterLibFunc(&lib.mixSum, h, "t_mix_sum")
	purego.RegisterLibFunc(&lib.big, h, "t_big")
	purego.RegisterLibFunc(&lib.bigSum, h, "t_big_sum")
	purego.RegisterLibFunc(&lib.pad, h, "t_pad")
	purego.RegisterLibFunc(&lib.cbInt, h, "t_cb_int")
	purego.RegisterLibFunc(&lib.cbFloat, h, "t_cb_float")
	purego.RegisterLibFunc(&lib.cbMany, h, "t_cb_many")
	purego.RegisterLibFunc(&lib.cbFmany, h, "t_cb_fmany")
	purego.RegisterLibFunc(&lib.cbSmall, h, "t_cb_small")
	purego.RegisterLibFunc(&lib.cbStr, h, "t_cb_str")
	purego.RegisterLibFunc(&lib.cbBool, h, "t_cb_bool")

	fmt.Println("ints:", lib.i8(100, 27), lib.i8(-100, -29), lib.u8(90), lib.i16(-30000, 2768),
		lib.u16(0x1fff), lib.i32(-7, 6), lib.u32(5), lib.i64(1<<40, -3), lib.u64(1), lib.iptr(42))
	fmt.Println("bools:", lib.boolean(true, false), lib.boolean(true, true), lib.boolean(false, false))
	fmt.Println("floats:", lib.f32(1.25, 4), lib.f64(1, 3), lib.mixed(-2, 0.25, 3, 1.5, -1, 65535))
	fmt.Println("stack:", lib.manyInts(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12),
		lib.manyFloats(0.5, 1, 1.5, 2, 2.5, 3, 3.5, 4, 4.5, 5, 5.5),
		lib.stackSmall(1, 2, 3, 4, 5, 6, 7, 8, -9, 3, -2, 1<<33))
	fmt.Println("strings:", lib.upper("hello, purego"), lib.strlen("marmot"))
	buf := make([]int32, 6)
	lib.fill(&buf[0], int32(len(buf)))
	p := lib.ptr(unsafe.Pointer(&buf[0]), 8)
	fmt.Println("pointers:", buf, *(*int32)(p), uintptr(p)-uintptr(unsafe.Pointer(&buf[0])))
	fmt.Println("structs:", lib.small(4, 5), lib.smallSum(small{3, 4}), lib.hfa(1.5), lib.hfaSum(hfa{1, 2, 3, 4}))
	fmt.Println("structs:", lib.dbl2(2.5), lib.mix(21, 3), lib.mixSum(mix{40, 0.5}), lib.big(10), lib.bigSum(big{1, 2, 3, 4}), lib.pad('z', -5, 300))

	add := purego.NewCallback(func(a, b int64) int64 { return a*100 + b })
	fl := purego.NewCallback(func(a float64, b float32, c int32) int64 { return int64((a + float64(b)*float64(c)) * 10) })
	many := purego.NewCallback(func(a1, a2, a3, a4, a5, a6, a7, a8, a9, a10 int64) int64 {
		return a1 + a2*2 + a3*3 + a4*4 + a5*5 + a6*6 + a7*7 + a8*8 + a9*9 + a10*10
	})
	fmany := purego.NewCallback(func(a1, a2, a3, a4, a5, a6, a7, a8, a9, a10 float64) int64 {
		return int64((a1 + a2*2 + a3*3 + a4*4 + a5*5 + a6*6 + a7*7 + a8*8 + a9*9 + a10*10) * 10)
	})
	sm := purego.NewCallback(func(s small) small { return small{s.B * 2, s.A + 3} })
	str := purego.NewCallback(func(s *byte) int32 {
		n := int32(0)
		for p := unsafe.Pointer(s); *(*byte)(p) != 0; p = unsafe.Add(p, 1) {
			n++
		}
		return n
	})
	fmt.Println("callbacks:", lib.cbInt(add, 3, 4), lib.cbFloat(fl, 0.5), lib.cbMany(many), lib.cbFmany(fmany),
		lib.cbSmall(sm, 6), lib.cbStr(str, "seven!!"), lib.cbBool(func(a bool, b int8) bool { return a && b < 0 }, true))

	libc, err := purego.Dlopen(libcPath(), purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	var qsort func(base unsafe.Pointer, n, size uintptr, cmp uintptr)
	purego.RegisterLibFunc(&qsort, libc, "qsort")
	cmp := purego.NewCallback(func(a, b unsafe.Pointer) int32 {
		x, y := *(*int64)(a), *(*int64)(b)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	})
	xs := []int64{5, -3, 9, 0, 2, 2, -8, 1 << 40}
	qsort(unsafe.Pointer(&xs[0]), uintptr(len(xs)), 8, cmp)
	fmt.Println("qsort:", xs)
	labs, err := purego.Dlsym(libc, "labs")
	if err != nil {
		panic(err)
	}
	r1, _, _ := purego.SyscallN(labs, ^uintptr(41))
	fmt.Println("SyscallN:", int64(r1))
	_, err = purego.Dlopen(filepath.Join(filepath.Dir(so), "missing.so"), purego.RTLD_NOW)
	fmt.Println("missing library:", err != nil)
	_, err = purego.Dlsym(h, "t_missing")
	fmt.Println("missing symbol:", err != nil)
	fmt.Println("close:", purego.Dlclose(h) == nil)
}
