// pcln.c: find the marmot pc table in the running binary.
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

// ---- type links (reflect type identity) ----

#if RT_OS_DARWIN
RT_API void **rt_typelinks(intptr_t *n) {
  unsigned long size = 0;
  uint8_t *p = getsectiondata(&_mh_execute_header, "__DATA", "__marmot_tlinks", &size);
  if (!p || size < sizeof(void *)) {
    *n = 0;
    return NULL;
  }
  *n = (intptr_t)(size / sizeof(void *));
  return (void **)p;
}
#elif RT_OS_LINUX
// The linker defines these for a section named like a C identifier. The
// link uses -z start-stop-gc, so these references keep no entry alive.
extern void *__start_marmot_tlinks[] __attribute__((weak, visibility("hidden")));
extern void *__stop_marmot_tlinks[] __attribute__((weak, visibility("hidden")));
RT_API void **rt_typelinks(intptr_t *n) {
  if (!__start_marmot_tlinks || !__stop_marmot_tlinks || __stop_marmot_tlinks <= __start_marmot_tlinks) {
    *n = 0;
    return NULL;
  }
  *n = __stop_marmot_tlinks - __start_marmot_tlinks;
  return __start_marmot_tlinks;
}
#else
RT_API void **rt_typelinks(intptr_t *n) {
  *n = 0;
  return NULL;
}
#endif

// ---- stack maps (pc table version 4) ----
//
// Version 4 adds, after the version 3 header fields, nsm, smIdxOff and
// smOff: nsm pairs of u32 (function index, offset into the blob at smOff),
// sorted by function index. A function's blob: uvarint nmaps; per map
// uvarint nwords and ceil(nwords/8) bytes, bit i (LSB first) set when the
// word at fp - 8*(i+1) may hold a pointer; uvarint nsites; per site, sorted
// by pc, uvarint delta of the return pc offset (from the function start for
// the first site) and uvarint map id. Every read is bounds checked: a bad
// table means no map (the frame is scanned conservatively).

static uint32_t rd32(const uint8_t *t, uintptr_t off) {
  return (uint32_t)t[off] | (uint32_t)t[off + 1] << 8 | (uint32_t)t[off + 2] << 16 | (uint32_t)t[off + 3] << 24;
}

// The uvarint at *off (below end), advancing *off; false past end or too long.
static bool rdvar(const uint8_t *t, uintptr_t end, uintptr_t *off, uint32_t *v) {
  uint32_t x = 0;
  for (unsigned s = 0; s < 35; s += 7) {
    if (*off >= end) return false;
    uint8_t c = t[(*off)++];
    x |= (uint32_t)(c & 0x7f) << s;
    if (c < 0x80) {
      *v = x;
      return true;
    }
  }
  return false;
}

// The map of the frame suspended at return address pc in table t (len
// bytes, offsets from base): its word count, with *bits its bitmap; 0 when
// pc has none.
RT_API uint32_t rt_pcln_stackmap(const uint8_t *t, intptr_t len, uintptr_t base, uintptr_t pc,
                                 const uint8_t **bits) {
  uintptr_t n = (uintptr_t)len;
  if (!t || n < 84 || __builtin_memcmp(t, "MRMTPCLN", 8) != 0 || rd32(t, 8) != 4) return 0;
  uint32_t nfuncs = rd32(t, 12), text_end = rd32(t, 32), func_off = rd32(t, 36);
  uint32_t nsm = rd32(t, 72), idx_off = rd32(t, 76), sm_off = rd32(t, 80);
  if (!nfuncs || !nsm || (uintptr_t)func_off + (uintptr_t)nfuncs * 12 > n ||
      (uintptr_t)idx_off + (uintptr_t)nsm * 8 > n || sm_off >= n)
    return 0;
  if (pc < base || pc - base >= text_end) return 0;
  uint32_t off = (uint32_t)(pc - base);
  if (off < rd32(t, func_off)) return 0;
  uint32_t lo = 0, hi = nfuncs;  // start(lo) <= off < start(hi)
  while (hi - lo > 1) {
    uint32_t mid = lo + (hi - lo) / 2;
    if (rd32(t, func_off + 12 * (uintptr_t)mid) <= off) lo = mid;
    else hi = mid;
  }
  uint32_t fn = lo, foff = off - rd32(t, func_off + 12 * (uintptr_t)fn);
  uint32_t a = 0, b = nsm;
  while (a < b) {
    uint32_t mid = a + (b - a) / 2;
    uint32_t k = rd32(t, idx_off + 8 * (uintptr_t)mid);
    if (k == fn) {
      a = mid;
      b = mid + 1;
      break;
    }
    if (k < fn) a = mid + 1;
    else b = mid;
  }
  if (a >= nsm || rd32(t, idx_off + 8 * (uintptr_t)a) != fn) return 0;
  uintptr_t p = (uintptr_t)sm_off + rd32(t, idx_off + 8 * (uintptr_t)a + 4);
  uint32_t nmaps, nw, nsites, d, id;
  if (p >= n || !rdvar(t, n, &p, &nmaps)) return 0;
  uintptr_t maps = p;
  for (uint32_t i = 0; i < nmaps; i++) {
    if (!rdvar(t, n, &p, &nw)) return 0;
    p += ((uintptr_t)nw + 7) / 8;
    if (p > n) return 0;
  }
  if (!rdvar(t, n, &p, &nsites)) return 0;
  uint32_t at = 0;
  for (uint32_t i = 0; i < nsites; i++) {
    if (!rdvar(t, n, &p, &d) || !rdvar(t, n, &p, &id)) return 0;
    at += d;
    if (at > foff) return 0;  // sorted: no site at foff
    if (at < foff) continue;
    if (id >= nmaps) return 0;
    p = maps;
    for (uint32_t m = 0;; m++) {
      if (!rdvar(t, n, &p, &nw)) return 0;
      if (m == id) {
        if (p + ((uintptr_t)nw + 7) / 8 > n) return 0;
        *bits = t + p;
        return nw;
      }
      p += ((uintptr_t)nw + 7) / 8;
    }
  }
  return 0;
}

// The running binary's table, when it has stack maps (version 4).
static int sm_state;  // 0 not looked, 1 no maps, 2 maps
static const uint8_t *sm_t;
static intptr_t sm_len;
static uintptr_t sm_base;

bool pcln_has_stackmaps(void) {
  int s = __atomic_load_n(&sm_state, __ATOMIC_ACQUIRE);
  if (!s) {
    const uint8_t *t;
    intptr_t len;
    uintptr_t base;
    s = 1;
    if (rt_pcln(&t, &len, &base) && len >= 84 && rd32(t, 8) == 4) {
      sm_t = t, sm_len = len, sm_base = base;
      s = 2;
    }
    __atomic_store_n(&sm_state, s, __ATOMIC_RELEASE);
  }
  return s == 2;
}

// The map at pc in the running binary (0: none). pcln_has_stackmaps first.
uint32_t pcln_stackmap(uintptr_t pc, const uint8_t **bits) {
  return rt_pcln_stackmap(sm_t, sm_len, sm_base, pc, bits);
}
