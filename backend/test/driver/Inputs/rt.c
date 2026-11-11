// Stand-in for runtime/core: one small pure function, one that touches
// internal state, one that touches external state, one noinline.
static long counter;
long rt_state_ext;
long rt_add(long a, long b) { return a + b; }
long rt_count(void) { return ++counter; }
long rt_ext(void) { return ++rt_state_ext; }
__attribute__((noinline)) long rt_slow(long a) { return a * 3; }
static long helper(long a) { return a << 1; }
long rt_twice(long a) { return helper(a); }
