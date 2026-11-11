long long mainadd(long long, long long) __asm__("_main.add");
int main(int argc, char **argv) { return (int)mainadd(argc, 2); }
