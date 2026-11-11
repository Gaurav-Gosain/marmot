// saxpy for the GPU path (docs/gpu.md): run with MARMOT_GPU=1 after
// marmot build -release with MARMOT_BACKEND_FLAGS=-gpu on a CUDA host.
package main

const n = 1 << 24

func saxpy(a float64, x, y []float64) {
	for i := range x {
		y[i] = a*x[i] + y[i]
	}
}

func main() {
	x := make([]float64, n)
	y := make([]float64, n)
	for i := range x {
		x[i] = float64(i)
		y[i] = 1
	}
	for r := 0; r < 20; r++ {
		saxpy(2, x, y)
	}
	bad := 0
	for i := range y {
		if y[i] != 40*float64(i)+1 {
			bad++
		}
	}
	println("saxpy", n, "wrong", bad, y[12345])
}
