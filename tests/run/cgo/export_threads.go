// cgo: C threads that the Go runtime did not create call exported Go
// functions.
package main

/*
#include <pthread.h>
extern void goVisit(int, int);
static void *worker(void *arg) {
	int id = (int)(long)arg;
	for (int i = 0; i < 100; i++) goVisit(id, i);
	return 0;
}
static void run(int n) {
	pthread_t t[16];
	for (int i = 0; i < n; i++) pthread_create(&t[i], 0, worker, (void *)(long)i);
	for (int i = 0; i < n; i++) pthread_join(t[i], 0);
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"sync"
)

var (
	mu     sync.Mutex
	visits = map[int]int{}
	sink   [][]byte
)

//export goVisit
func goVisit(id, i C.int) {
	b := make([]byte, 1024) // allocate on the foreign thread
	b[0] = byte(i)
	mu.Lock()
	visits[int(id)] += int(b[0]) + 1
	sink = append(sink, b)
	if len(sink) > 100 {
		sink = nil
	}
	mu.Unlock()
	if i == 50 {
		runtime.GC()
	}
}

func main() {
	C.run(8)
	for id := 0; id < 8; id++ {
		fmt.Print(visits[id], " ")
	}
	fmt.Println()
}
