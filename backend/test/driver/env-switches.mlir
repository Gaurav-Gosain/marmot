// Every environment variable the backend reads is listed in
// backend/include/marmot/Driver/EnvSwitches.def, and each switch listed as
// changing objects is part of the object cache key.
// RUN: python3 %S/Inputs/env_switches.py %S/../../.. %backend %t | %FileCheck %s
// CHECK: env-switches: {{[0-9]+}} names, 0 problems
module {
}
