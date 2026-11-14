// Watchdog.h: resource ceiling for marmot-backend.
#ifndef MARMOT_DRIVER_WATCHDOG_H
#define MARMOT_DRIVER_WATCHDOG_H

#include <string>

namespace marmot {

// Starts a background thread that ends the process with exit status 3 and
// a message on stderr when
//  - resident plus compressed (macOS phys_footprint) or swapped (Linux)
//    memory passes MARMOT_MAX_MEM_MB (default 3072),
//  - wall time passes MARMOT_MAX_SECONDS (default 300),
// and that exits quietly with status 3 when the parent process dies. A limit
// of 0 turns that check off.
void startWatchdog();

// The memory the process uses now, as the watchdog counts it (resident
// and swapped on Linux, the physical footprint on macOS); 0 if unknown.
uint64_t processMemory();

// --serve: the time limit counts from the start of a request; idle time
// between requests does not count.
void watchdogRequest(bool active);

// The inputs being compiled, named in the watchdog message.
void watchdogBegin(const std::string &input);
void watchdogEnd(const std::string &input);

} // namespace marmot

#endif
