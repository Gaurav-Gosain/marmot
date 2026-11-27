// modinfo.S: weak defaults for the build info (runtime/debug.ReadBuildInfo).
// When the driver stamps build info, it links an object with strong
// definitions of marmot_modinfo and marmot_modinfo_len (internal/driver/
// buildinfo.go); a strong definition wins and this archive member is not
// loaded. Without that object, rt_modinfo reads these: no build info.
// The defaults are in assembly, not C, so rtcore.bc carries no initializer
// that the backend could fold into an inlined rt_modinfo. Both are hidden:
// an exported weak definition makes dyld coalesce weak symbols at every
// launch (Mach-O) and exports them from shared builds (ELF).

#if defined(__APPLE__)
	.section __TEXT,__const
	.globl _marmot_modinfo
	.weak_definition _marmot_modinfo
	.private_extern _marmot_modinfo
_marmot_modinfo:
	.byte 0
	.p2align 3
	.globl _marmot_modinfo_len
	.weak_definition _marmot_modinfo_len
	.private_extern _marmot_modinfo_len
_marmot_modinfo_len:
	.quad 0
#else
	.section .rodata.marmot_modinfo,"a"
	.weak marmot_modinfo
	.hidden marmot_modinfo
	.type marmot_modinfo, @object
marmot_modinfo:
	.byte 0
	.size marmot_modinfo, 1
	.p2align 3
	.weak marmot_modinfo_len
	.hidden marmot_modinfo_len
	.type marmot_modinfo_len, @object
marmot_modinfo_len:
	.quad 0
	.size marmot_modinfo_len, 8
	.section .note.GNU-stack,"",@progbits
#endif
