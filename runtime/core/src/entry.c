// entry.c: process entry, exit, fatal errors, arguments and environment.

#include "internal.h"

#if RT_OS_DARWIN
#include <stdlib.h>  // exit
#endif

static int32_t g_argc;
static char **g_argv;
static char **g_envp;

RT_API int32_t rt_argc(void) { return g_argc; }
RT_API char **rt_argv(void) { return g_argv; }
RT_API char **rt_envp(void) { return g_envp; }

// A normal exit (os.Exit, main returning). On darwin it goes through libc
// exit, as gc does: C stdio buffers of cgo and purego code are flushed and
// atexit handlers run (output piped to a file was lost with _exit). Linux
// exits directly, as gc does there. Fatal errors use os_exit, which never
// runs user code (it can be reached from a signal handler).
RT_API void rt_exit(int32_t code) {
#if RT_OS_DARWIN
  exit(code);
#else
  os_exit(code);
#endif
}

RT_API int32_t rt_output_json(void) {
  const char *v = rt_getenv("MARMOT_OUTPUT");
  return v && v[0] == 'j' && v[1] == 's' && v[2] == 'o' && v[3] == 'n' && v[4] == 0;
}

// json_string prints s as a JSON string literal.
static void json_string(const char *s, uintptr_t n) {
  static const char hex[] = "0123456789abcdef";
  rt_print("\"");
  for (uintptr_t i = 0; i < n; i++) {
    unsigned char c = (unsigned char)s[i];
    if (c == '"' || c == '\\') {
      char b[2] = {'\\', (char)c};
      rt_printn(b, 2);
    } else if (c < 0x20) {
      char b[6] = {'\\', 'u', '0', '0', hex[c >> 4], hex[c & 15]};
      rt_printn(b, 6);
    } else {
      rt_printn((const char *)&c, 1);
    }
  }
  rt_print("\"");
}

// A fatal error: "fatal error: msg" and exit status 2, or with
// MARMOT_OUTPUT=json one JSON object (schema 1, kind fatal).
RT_API void rt_fatal(const char *msg, uintptr_t len) {
  if (rt_output_json()) {
    int64_t st[3];
    rt_gstates(st);
    rt_print("{\"schema\":1,\"kind\":\"fatal\",\"code\":\"M0021\",\"message\":");
    json_string(msg, len);
    rt_print(",\"goroutines\":{\"running\":");
    rt_printu((uint64_t)st[0]);
    rt_print(",\"runnable\":");
    rt_printu((uint64_t)st[1]);
    rt_print(",\"waiting\":");
    rt_printu((uint64_t)st[2]);
    rt_print("},\"exitCode\":2}\n");
    os_exit(2);
  }
  rt_print("fatal error: ");
  rt_printn(msg, len);
  rt_print("\n");
  os_exit(2);
}

#if RT_OS_LINUX
// gpu.c: defined only when the program links rt_gpu_for (weak reference,
// does not pull gpu.o from the archive).
extern void rt_gpu_prewarm(void) __attribute__((weak));
#endif

static void rt_init(int argc, char **argv, char **envp) {
  g_argc = argc;
  g_argv = argv;
  g_envp = envp;
  os_init();
  malloc_init();
  sched_init();
  gc_init();
#if RT_OS_LINUX
  if (rt_gpu_prewarm) rt_gpu_prewarm();
#endif
}

RT_API int main(int argc, char **argv, char **envp) {
  rt_init(argc, argv, envp);
  sched_start_main();
}
