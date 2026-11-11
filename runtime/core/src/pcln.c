// pcln.c: find the marmot pc table (docs/pcln.md) in the running binary.
// Safe to call while panicking with a corrupted heap: no heap allocation,
// one static initialization (system calls only), result cached.

#include "internal.h"

#if RT_OS_DARWIN
#include <mach-o/getsect.h>
#include <mach-o/ldsyms.h>
#elif RT_OS_LINUX
#include <elf.h>
#include <fcntl.h>
#include <link.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>
#endif

static int pcln_state;  // 0 not tried, 1 in progress, 2 done
static const uint8_t *pcln_table;
static intptr_t pcln_len;
static uintptr_t pcln_base;

#if RT_OS_DARWIN
static void pcln_find(void) {
  const struct mach_header_64 *mh = &_mh_execute_header;
  unsigned long size = 0;
  uint8_t *p = getsectiondata(mh, "__TEXT", "__marmot_pcln", &size);
  if (!p || !size) p = getsectiondata(mh, "__MARMOT", "__pcln", &size);
  if (!p || !size) return;
  pcln_table = p;
  pcln_len = (intptr_t)size;
  pcln_base = (uintptr_t)mh;  // __TEXT vmaddr + slide
}
#elif RT_OS_LINUX
static int base_cb(struct dl_phdr_info *info, size_t size, void *data) {
  (void)size;
  uintptr_t lo = UINTPTR_MAX;
  for (int i = 0; i < info->dlpi_phnum; i++) {
    const ElfW(Phdr) *ph = &info->dlpi_phdr[i];
    if (ph->p_type == PT_LOAD && ph->p_vaddr < lo) lo = ph->p_vaddr;
  }
  *(uintptr_t *)data = info->dlpi_addr + (lo == UINTPTR_MAX ? 0 : lo);
  return 1;  // the main program only
}

static void pcln_find(void) {
  int fd = open("/proc/self/exe", O_RDONLY | O_CLOEXEC);
  if (fd < 0) return;
  struct stat st;
  if (fstat(fd, &st) != 0 || st.st_size < (off_t)sizeof(ElfW(Ehdr))) {
    close(fd);
    return;
  }
  uintptr_t fsize = (uintptr_t)st.st_size;
  const uint8_t *f = mmap(NULL, fsize, PROT_READ, MAP_PRIVATE, fd, 0);
  close(fd);
  if (f == MAP_FAILED) return;
  const ElfW(Ehdr) *eh = (const ElfW(Ehdr) *)f;
  bool ok = eh->e_ident[0] == 0x7f && eh->e_ident[1] == 'E' && eh->e_shoff &&
            eh->e_shentsize == sizeof(ElfW(Shdr)) && eh->e_shstrndx < eh->e_shnum &&
            eh->e_shoff + (uintptr_t)eh->e_shnum * sizeof(ElfW(Shdr)) <= fsize;
  if (ok) {
    const ElfW(Shdr) *sh = (const ElfW(Shdr) *)(f + eh->e_shoff);
    const ElfW(Shdr) *strs = &sh[eh->e_shstrndx];
    static const char want[] = ".marmot_pcln";
    for (int i = 0; ok && i < eh->e_shnum; i++) {
      uintptr_t no = strs->sh_offset + sh[i].sh_name;
      if (no + sizeof want > fsize || __builtin_memcmp(f + no, want, sizeof want) != 0) continue;
      if (sh[i].sh_type == SHT_NOBITS || sh[i].sh_offset + sh[i].sh_size > fsize) break;
      uintptr_t base = 0;
      dl_iterate_phdr(base_cb, &base);
      pcln_table = f + sh[i].sh_offset;
      pcln_len = (intptr_t)sh[i].sh_size;
      pcln_base = base;
      return;  // keep the file mapped for the life of the process
    }
  }
  munmap((void *)f, fsize);
}
#else
static void pcln_find(void) {}
#endif

RT_API int32_t rt_pcln(const uint8_t **table, intptr_t *len, uintptr_t *base) {
  int s = __atomic_load_n(&pcln_state, __ATOMIC_ACQUIRE);
  if (s != 2) {
    int z = 0;
    if (__atomic_compare_exchange_n(&pcln_state, &z, 1, false, __ATOMIC_ACQ_REL, __ATOMIC_ACQUIRE)) {
      pcln_find();
      __atomic_store_n(&pcln_state, 2, __ATOMIC_RELEASE);
    } else {
      while (__atomic_load_n(&pcln_state, __ATOMIC_ACQUIRE) != 2) os_yield();
    }
  }
  if (!pcln_table) return 0;
  *table = pcln_table;
  *len = pcln_len;
  *base = pcln_base;
  return 1;
}
