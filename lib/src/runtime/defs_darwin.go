// OS constants for darwin. Compiled only by marmot.

package runtime

import "internal/goarch"

// physPageSize is the system page size: 16 KiB on arm64, 4 KiB on amd64.
const physPageSize = 4096 << (2 * (goarch.IsArm64))

const _ENOTSUP = 45
