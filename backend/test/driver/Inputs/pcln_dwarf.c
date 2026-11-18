// pcln-dwarf.mlir: two functions with Go names in two packages, and one
// without a package (_start).
int g;

__attribute__((noinline)) int energy(int x) __asm__("main.energy");
__attribute__((noinline)) int energy(int x) {
  g += x;
  g *= 3;
  return g;
}

__attribute__((noinline)) int sum(int x) __asm__("example.com/m/pkg.Sum");
__attribute__((noinline)) int sum(int x) {
  g -= x;
  return g + 1;
}

void _start(void) {
  energy(1);
  sum(2);
  for (;;) {
  }
}
