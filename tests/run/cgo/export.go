// cgo: //export, C calling Go: scalars, structs, several results, nested
// calls Go -> C -> Go -> C, and a callback that blocks in Go.
package main

/*
#include <stdint.h>
typedef struct { int a; double b; } pt;
extern int goAdd(int, int);
extern pt goPt(pt);
extern int64_t goMix(int8_t, unsigned short, char *);
extern void goNote(int);
extern int goDeep(int);
extern int goWait(int);
static int callAdd(int x) { return goAdd(x, 1); }
static pt callPt(int a) { pt p = {a, 0.5}; return goPt(p); }
static long long callMix(void) { char c = 'x'; return goMix(-5, 65535, &c); }
static void callNote(int n) { for (int i = 0; i < n; i++) goNote(i); }
static int deep(int n) { return n == 0 ? 0 : 1 + goDeep(n - 1); }
static int callDeep(int n) { return deep(n); }
static int callWait(int n) { int s = 0; for (int i = 0; i < n; i++) s += goWait(i); return s; }
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
)

//export goAdd
func goAdd(a, b C.int) C.int { return a + b }

//export goPt
func goPt(p C.pt) C.pt {
	p.a++
	p.b *= 4
	return p
}

//export goMix
func goMix(a C.int8_t, u C.ushort, p *C.char) C.int64_t {
	return C.int64_t(a)*100000 + C.int64_t(u)*10 + C.int64_t(*p)
}

var notes []int

//export goNote
func goNote(i C.int) { notes = append(notes, int(i)) }

//export goDeep
func goDeep(n C.int) C.int { return C.deep(n) }

var ch = make(chan int)

//export goWait
func goWait(i C.int) C.int {
	// Block in Go inside a callback: another goroutine answers.
	ch <- int(i)
	time.Sleep(time.Millisecond)
	return <-ch2
}

var ch2 = make(chan C.int)

func main() {
	fmt.Println(C.callAdd(41))
	p := C.callPt(3)
	fmt.Println(p.a, float64(p.b))
	fmt.Println(C.callMix())
	C.callNote(5)
	fmt.Println(notes)
	fmt.Println(C.callDeep(200))
	go func() {
		for v := range ch {
			ch2 <- C.int(v * 2)
		}
	}()
	fmt.Println(C.callWait(10))
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			s := 0
			for i := 0; i < 1000; i++ {
				s += int(C.callAdd(C.int(i)))
			}
			mu.Lock()
			total += s
			mu.Unlock()
		}(g)
	}
	wg.Wait()
	fmt.Println(total)
}
