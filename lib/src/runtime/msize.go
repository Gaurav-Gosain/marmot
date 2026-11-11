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
// literal tables (generated from classToSize), not built by an init
// function: allocation happens during package initialization, possibly
// before the runtime's init functions run.
var sizeToClass8 = [smallSizeMax/smallSizeDiv + 1]uint8{
	0, 1, 2, 3, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13, 14,
	14, 15, 15, 16, 16, 17, 17, 18, 18, 19, 19, 19, 19, 20, 20, 20, 20, 21, 21, 21, 21, 22, 22, 22,
	22, 23, 23, 23, 23, 24, 24, 24, 24, 25, 25, 25, 25, 26, 26, 26, 26, 27, 27, 27, 27, 27, 27, 27,
	27, 28, 28, 28, 28, 28, 28, 28, 28, 29, 29, 29, 29, 29, 29, 29, 29, 30, 30, 30, 30, 30, 30, 30,
	30, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 32, 32, 32, 32, 32, 32, 32,
	32, 32, 32, 32, 32, 32, 32, 32, 32,
}

var sizeToClass128 = [(maxSmallSize-smallSizeMax)/largeSizeDiv + 1]uint8{
	32, 33, 34, 35, 36, 37, 37, 38, 38, 39, 39, 40, 40, 40, 41, 41, 41, 42, 43, 43, 44, 44, 44, 44,
	44, 45, 45, 45, 45, 45, 45, 46, 46, 46, 46, 47, 47, 47, 47, 47, 47, 48, 48, 48, 49, 49, 50, 51,
	51, 51, 51, 51, 51, 51, 51, 51, 51, 52, 52, 52, 52, 52, 52, 52, 52, 52, 52, 53, 53, 54, 54, 54,
	54, 55, 55, 55, 55, 55, 56, 56, 56, 56, 56, 56, 56, 56, 56, 56, 56, 57, 57, 57, 57, 57, 57, 57,
	57, 57, 57, 58, 58, 58, 58, 58, 58, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59,
	59, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 60, 61, 61, 61, 61, 61, 62, 62,
	62, 62, 62, 62, 62, 62, 62, 62, 62, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 64, 64, 64, 64, 64,
	64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 65, 65, 65, 65, 65, 65, 65,
	65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66,
	66, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67, 67,
	67, 67, 67, 67, 67, 67, 67, 67, 67,
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
