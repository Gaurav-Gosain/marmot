// Allocation size rounding, matching the gc runtime's size classes so that
// append growth and []byte(s) capacities print the same values as with the
// gc toolchain. Pure file.

package runtime

const (
	maxSmallSize           = 32768
	smallSizeDiv           = 8
	smallSizeMax           = 1024
	largeSizeDiv           = 128
	pageSize               = 8192
	mallocHeaderSize       = 8
	minSizeForMallocHeader = 512
)

var classToSize = [...]uint16{0, 8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256, 288, 320, 352, 384, 416, 448, 480, 512, 576, 640, 704, 768, 896, 1024, 1152, 1280, 1408, 1536, 1792, 2048, 2304, 2688, 3072, 3200, 3456, 4096, 4864, 5376, 6144, 6528, 6784, 6912, 8192, 9472, 9728, 10240, 10880, 12288, 13568, 14336, 16384, 18432, 19072, 20480, 21760, 24576, 27264, 28672, 32768}

// sizeToClass8 maps (n+7)/8 to the size class index for n <= 1024, and
// sizeToClass128 maps (n-1024+127)/128 for 1024 < n <= 32768. They are
// built from classToSize at init.
var (
	sizeToClass8   [smallSizeMax/smallSizeDiv + 1]uint8
	sizeToClass128 [(maxSmallSize-smallSizeMax)/largeSizeDiv + 1]uint8
)

func init() {
	c := 0
	for i := range sizeToClass8 {
		for int(classToSize[c]) < i*smallSizeDiv {
			c++
		}
		sizeToClass8[i] = uint8(c)
	}
	for i := range sizeToClass128 {
		n := smallSizeMax + i*largeSizeDiv
		for int(classToSize[c]) < n {
			c++
		}
		sizeToClass128[i] = uint8(c)
	}
}

// sizeClassUp returns the smallest size class >= n, for n <= maxSmallSize.
func sizeClassUp(n uintptr) uintptr {
	if n <= smallSizeMax-8 {
		return uintptr(classToSize[sizeToClass8[(n+smallSizeDiv-1)/smallSizeDiv]])
	}
	return uintptr(classToSize[sizeToClass128[(n-smallSizeMax+largeSizeDiv-1)/largeSizeDiv]])
}

// roundupsize returns the size of the memory block that the gc runtime
// would allocate for a request of size bytes. noscan reports whether the
// object holds no pointers.
func roundupsize(size uintptr, noscan bool) uintptr {
	reqSize := size
	if reqSize <= maxSmallSize-mallocHeaderSize {
		if !noscan && reqSize > minSizeForMallocHeader {
			reqSize += mallocHeaderSize
		}
		return sizeClassUp(reqSize) - (reqSize - size)
	}
	reqSize += pageSize - 1
	if reqSize < size {
		return size
	}
	return reqSize &^ (pageSize - 1)
}
