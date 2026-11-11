// Inlined frames for the version 3 pc table.
volatile int sink;
static inline __attribute__((always_inline)) int leaf(int x) {
  sink = x;
  return x * 7;
}
static inline __attribute__((always_inline)) int mid(int x) {
  sink = x + 1;
  return leaf(x + 2) + 1;
}
__attribute__((noinline)) int outer(int x) {
  sink = 0;
  return mid(x) + 3;
}
int main(int argc, char **argv) { return outer(argc); }
