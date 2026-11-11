long long mainadd(long long, long long) __asm__("_main.add");
// A function without DWARF lines whose name uses a main package path.
__attribute__((noinline)) int helper(int x) __asm__("_example.com/p.helper");
int helper(int x) { return x * 3; }
int main(int argc, char **argv) { return (int)mainadd(helper(argc), 2); }
