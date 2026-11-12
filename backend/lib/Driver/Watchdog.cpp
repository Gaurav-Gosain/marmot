// Watchdog.cpp: see Watchdog.h.
//
// OS FILE: memory measurement and parent death detection are per OS
// (macOS and Linux below).
#include "marmot/Driver/Watchdog.h"

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
  std::fflush(stderr);
  _exit(3);
}

// Start of the current request in steady clock nanoseconds; 0 while idle.
std::atomic<int64_t> requestStart{-1};

} // namespace

void watchdogRequest(bool active) {
  requestStart = active ? std::chrono::steady_clock::now().time_since_epoch().count() : 0;
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

void startWatchdog() {
  uint64_t maxMB = envLimit("MARMOT_MAX_MEM_MB", 3072);
  uint64_t maxSec = envLimit("MARMOT_MAX_SECONDS", 300);
  pid_t parent = getppid();
#if defined(__linux__)
  // The kernel kills us when the parent thread exits; the poll below covers
  // the race where the parent died before this call.
  prctl(PR_SET_PDEATHSIG, SIGKILL);
#endif
  auto start = std::chrono::steady_clock::now();
  std::thread([=] {
    for (;;) {
      std::this_thread::sleep_for(std::chrono::milliseconds(200));
      if (getppid() != parent)
        _exit(3); // parent died, we were reparented
      if (maxMB) {
        uint64_t mb = memoryInUse() >> 20;
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
        if ((uint64_t)secs > maxSec)
          die("time", secs, maxSec, "s", "MARMOT_MAX_SECONDS");
      }
    }
  }).detach();
}

} // namespace marmot
