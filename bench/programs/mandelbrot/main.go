// mandelbrot: float loops with early exit. Counts points inside the set.
package main

const size = 15000

func main() {
	inside := 0
	for py := 0; py < size; py++ {
		ci := 2.0*float64(py)/size - 1.0
		for px := 0; px < size; px++ {
			cr := 2.0*float64(px)/size - 1.5
			zr, zi := 0.0, 0.0
			i := 0
			for ; i < 50 && zr*zr+zi*zi <= 4.0; i++ {
				zr, zi = zr*zr-zi*zi+cr, 2*zr*zi+ci
			}
			if i == 50 {
				inside++
			}
		}
	}
	println("mandelbrot", inside)
}
