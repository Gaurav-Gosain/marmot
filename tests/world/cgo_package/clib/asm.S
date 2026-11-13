// clib_asm_add(a, b) = a + b, assembled by the C compiler as cgo does.
#if defined(__APPLE__)
#define SYM(x) _##x
#else
#define SYM(x) x
#endif
	.text
	.globl SYM(clib_asm_add)
#if defined(__aarch64__)
	.p2align 2
SYM(clib_asm_add):
	add x0, x0, x1
	ret
#elif defined(__x86_64__)
	.p2align 4
SYM(clib_asm_add):
	leaq (%rdi,%rsi), %rax
	ret
#endif
#if defined(__linux__)
	.section .note.GNU-stack,"",@progbits
#endif
