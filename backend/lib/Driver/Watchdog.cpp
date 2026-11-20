// Watchdog.cpp: see Watchdog.h.
//
// OS FILE: memory measurement and parent death detection are per OS
// (macOS and Linux below).
#include "marmot/Driver/Watchdog.h"

#include <algorithm>
#include <atomic>
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <mutex>
#include <set>
#include <string>
#include <thread>
#include <unistd.h>

#if defined(__APPLE__)
#include <mach/mach.h>
#include <malloc/malloc.h>
#include <sys/sysctl.h>
#elif defined(__linux__)
#include <csignal>
#include <fstream>
#include <sys/prctl.h>
#endif

namespace marmot {

namespace {

std::mutex mu;
std::multiset<std::string> inFlight;

uint64_t envLimit(const char *name, uint64_t def) {
  const char *v = std::getenv(name);
  if (!v || !*v)
    return def;
  char *end = nullptr;
  unsigned long long n = std::strtoull(v, &end, 10);
  return (end && *end == 0) ? n : def;
}

// The machine's physical memory in bytes; 0 if unknown.
uint64_t hostMemory() {
#if defined(__APPLE__)
  uint64_t mem = 0;
  size_t len = sizeof(mem);
  if (sysctlbyname("hw.memsize", &mem, &len, nullptr, 0) != 0)
    return 0;
  return mem;
#elif defined(__linux__)
  long pages = sysconf(_SC_PHYS_PAGES), size = sysconf(_SC_PAGESIZE);
  return pages > 0 && size > 0 ? uint64_t(pages) * uint64_t(size) : 0;
#else
  return 0;
#endif
}

// The memory limit in MB. MARMOT_MAX_MEM_MB when set (0: no limit).
// Otherwise a quarter of the machine's memory, at least 3072 and at most
// 8192. The admission budgets in WholeProgram.cpp still plan against
// 3072 MB, so the extra room above that only absorbs estimate errors: a
// cold -release build of tuios on an 18 GB Mac passed 3072 MB once with
// two large split groups in flight.
uint64_t memoryLimitMB() {
  if (const char *v = std::getenv("MARMOT_MAX_MEM_MB"); v && *v)
    return envLimit("MARMOT_MAX_MEM_MB", 3072);
  uint64_t quarter = (hostMemory() >> 20) / 4;
  return std::max<uint64_t>(3072, std::min<uint64_t>(quarter, 8192));
}

// Memory charged to this process in bytes: what would have to be in RAM if
// nothing were compressed or swapped.
uint64_t memoryInUse() {
#if defined(__APPLE__)
  task_vm_info_data_t info;
  mach_msg_type_number_t count = TASK_VM_INFO_COUNT;
  if (task_info(mach_task_self(), TASK_VM_INFO, reinterpret_cast<task_info_t>(&info),
                &count) != KERN_SUCCESS)
    return 0;
  return info.phys_footprint;
#elif defined(__linux__)
  std::ifstream f("/proc/self/status");
  std::string key;
  uint64_t total = 0;
  while (f >> key) {
    uint64_t kb = 0;
    if (key == "VmRSS:" || key == "VmSwap:") {
      f >> kb;
      total += kb * 1024;
    }
    f.ignore(1 << 20, '\n');
  }
  return total;
#else
  return 0;
#endif
}

[[noreturn]] void die(const char *what, uint64_t value, uint64_t limit, const char *unit,
                      const char *env) {
  std::string names;
  {
    std::lock_guard<std::mutex> lock(mu);
    for (const std::string &s : inFlight) {
      if (!names.empty())
        names += ", ";
      names += s;
    }
  }
  std::fprintf(stderr,
               "marmot-backend: %s limit exceeded: %llu %s > %llu %s (%s) while "
               "compiling %s\n",
               what, (unsigned long long)value, unit, (unsigned long long)limit, unit,
               env, names.empty() ? "(no module)" : names.c_str());
  std::fprintf(stderr,
               "marmot-backend: to allow more, set %s to a higher value (for example "
               "%s=%llu), or set %s=0 to remove the limit.\n",
               env, env, (unsigned long long)(limit * 2), env);
  std::fflush(stderr);
  _exit(3);
}

// Start of the current request in steady clock nanoseconds; 0 while idle.
std::atomic<int64_t> requestStart{-1};

} // namespace

// Seconds added to the default time limit for the size of the input
// (watchdogAddTime; per request in --serve); not when MARMOT_MAX_SECONDS
// is set.
std::atomic<uint64_t> extraSec{0};

void watchdogAddTime(uint64_t secs) { extraSec += secs; }

void watchdogRequest(bool active) {
  requestStart = active ? std::chrono::steady_clock::now().time_since_epoch().count() : 0;
  extraSec = 0;
}

void watchdogBegin(const std::string &input) {
  std::lock_guard<std::mutex> lock(mu);
  inFlight.insert(input);
}

void watchdogEnd(const std::string &input) {
  std::lock_guard<std::mutex> lock(mu);
  auto it = inFlight.find(input);
  if (it != inFlight.end())
    inFlight.erase(it);
}



void startWatchdog(bool background) {
#if defined(__EMSCRIPTEN__)
  // No threads in the browser build (std::thread throws there); the page
  // stops the worker on its own timeout.
  (void)background;
  return;
#endif
  uint64_t maxMB = memoryLimitMB();
  // A background run (the tier 1 runner) outlives the build that started it
  // and runs at idle priority as long as it takes: no time limit and no
  // parent check, only the memory limit.
  uint64_t maxSec = background ? 0 : envLimit("MARMOT_MAX_SECONDS", 300);
  const bool scaled = std::getenv("MARMOT_MAX_SECONDS") == nullptr;
  const pid_t parent = background ? 0 : getppid();
#if defined(__linux__)
  // The kernel kills us when the parent thread exits; the poll below covers
  // the race where the parent died before this call.
  if (!background)
    prctl(PR_SET_PDEATHSIG, SIGKILL);
#endif
  auto start = std::chrono::steady_clock::now();
  std::thread([=] {
    for (;;) {
      std::this_thread::sleep_for(std::chrono::milliseconds(200));
      if (parent && getppid() != parent)
        _exit(3); // parent died, we were reparented
      if (maxMB) {
        uint64_t mb = memoryInUse() >> 20;
#if defined(__APPLE__)
        // The footprint counts pages malloc holds from freed memory; give
        // them back before judging (glibc builds trim after each job).
        if (mb > maxMB) {
          malloc_zone_pressure_relief(nullptr, 0);
          mb = memoryInUse() >> 20;
        }
#endif
        if (mb > maxMB)
          die("memory", mb, maxMB, "MB", "MARMOT_MAX_MEM_MB");
      }
      int64_t rs = requestStart.load();
      if (maxSec && rs != 0) {
        auto from = rs < 0 ? start
                           : std::chrono::steady_clock::time_point(
                                 std::chrono::steady_clock::duration(rs));
        auto secs = std::chrono::duration_cast<std::chrono::seconds>(
                        std::chrono::steady_clock::now() - from)
                        .count();
        uint64_t limit = maxSec + (scaled ? extraSec.load() : 0);
        if ((uint64_t)secs > limit)
          die("time", secs, limit, "s", "MARMOT_MAX_SECONDS");
      }
    }
  }).detach();
}

uint64_t processMemory() { return memoryInUse(); }

} // namespace marmot
