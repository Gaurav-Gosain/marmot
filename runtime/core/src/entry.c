// entry.c: process entry, exit, fatal errors, arguments and environment.

#include "internal.h"

static int32_t g_argc;
static char **g_argv;
static char **g_envp;

RT_API int32_t rt_argc(void) { return g_argc; }
RT_API char **rt_argv(void) { return g_argv; }
RT_API char **rt_envp(void) { return g_envp; }

RT_API void rt_exit(int32_t code) { os_exit(code); }

RT_API void rt_fatal(const char *msg, uintptr_t len) {
  rt_print("fatal error: ");
  rt_printn(msg, len);
  rt_print("\n");
  os_exit(2);
}

static void rt_init(int argc, char **argv, char **envp) {
  g_argc = argc;
  g_argv = argv;
  g_envp = envp;
  os_init();
  malloc_init();
  sched_init();
  timer_init();
  gc_init();
}

RT_API int main(int argc, char **argv, char **envp) {
  rt_init(argc, argv, envp);
  sched_start_main();
}
