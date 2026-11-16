// pcln-dead.mlir: dead() is removed by --gc-sections; its DWARF stays,
// with address 0.
int g;

__attribute__((noinline)) int live(int x) {
  g += x;
  return g;
}

int dead(int x) {
  return x * 42;
}

void _start(void) {
  live(1);
  for (;;) {
  }
}
