// Child processes start with SIGPIPE at its default action: the runtime
// handles SIGPIPE with a no-op handler (reset by exec), not SIG_IGN (which
// exec keeps). A shell that sends itself SIGPIPE must die from it.
// expect-stdout: ok
#include "rtest.h"
#include <signal.h>
#include <spawn.h>
#include <sys/wait.h>

extern char **environ;

void marmot_main(void) {
  struct sigaction sa;
  CHECK(sigaction(SIGPIPE, NULL, &sa) == 0 && sa.sa_handler != SIG_IGN);
  char *argv[] = {"/bin/sh", "-c", "kill -PIPE $$; exit 0", 0};
  pid_t pid;
  CHECK(posix_spawn(&pid, "/bin/sh", 0, 0, argv, environ) == 0);
  int st = 0;
  rt_entersyscall();
  pid_t w = waitpid(pid, &st, 0);
  rt_exitsyscall();
  CHECK(w == pid && WIFSIGNALED(st) && WTERMSIG(st) == SIGPIPE);
  out("ok\n");
}
