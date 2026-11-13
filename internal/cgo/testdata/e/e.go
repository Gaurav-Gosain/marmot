package e

/*
typedef struct { int a; double b; } pt;
extern int goAdd(int, int);
extern pt goPt(pt);
extern signed char goNeg(signed char);
static int callAdd(int x) { return goAdd(x, 1); }
*/
import "C"

//export goAdd
func goAdd(a, b C.int) C.int { return a + b }

//export goPt
func goPt(p C.pt) C.pt { return p }

//export goNeg
func goNeg(x C.schar) C.schar { return -x }

//export goTwo
func goTwo() (C.int, C.double) { return 1, 2 }

func Use() int { return int(C.callAdd(1)) }
