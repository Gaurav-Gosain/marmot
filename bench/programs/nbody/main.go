// nbody: float math on structs. Prints energy before and after.
package main

const steps = 2000000

type body struct {
	x, y, z, vx, vy, vz, mass float64
}

const (
	solarMass   = 4 * 3.141592653589793 * 3.141592653589793
	daysPerYear = 365.24
)

func sqrt(x float64) float64 {
	// Newton iteration so the program needs no imports.
	if x == 0 {
		return 0
	}
	z := x
	if z > 1 {
		z = x / 2
	}
	for i := 0; i < 30; i++ {
		z = 0.5 * (z + x/z)
	}
	return z
}

func energy(b []body) float64 {
	e := 0.0
	for i := range b {
		bi := &b[i]
		e += 0.5 * bi.mass * (bi.vx*bi.vx + bi.vy*bi.vy + bi.vz*bi.vz)
		for j := i + 1; j < len(b); j++ {
			bj := &b[j]
			dx, dy, dz := bi.x-bj.x, bi.y-bj.y, bi.z-bj.z
			e -= bi.mass * bj.mass / sqrt(dx*dx+dy*dy+dz*dz)
		}
	}
	return e
}

func advance(b []body, dt float64) {
	for i := range b {
		bi := &b[i]
		for j := i + 1; j < len(b); j++ {
			bj := &b[j]
			dx, dy, dz := bi.x-bj.x, bi.y-bj.y, bi.z-bj.z
			d2 := dx*dx + dy*dy + dz*dz
			mag := dt / (d2 * sqrt(d2))
			bi.vx -= dx * bj.mass * mag
			bi.vy -= dy * bj.mass * mag
			bi.vz -= dz * bj.mass * mag
			bj.vx += dx * bi.mass * mag
			bj.vy += dy * bi.mass * mag
			bj.vz += dz * bi.mass * mag
		}
	}
	for i := range b {
		bi := &b[i]
		bi.x += dt * bi.vx
		bi.y += dt * bi.vy
		bi.z += dt * bi.vz
	}
}

func main() {
	b := []body{
		{0, 0, 0, 0, 0, 0, solarMass},
		{4.84143144246472090e+00, -1.16032004402742839e+00, -1.03622044471123109e-01,
			1.66007664274403694e-03 * daysPerYear, 7.69901118419740425e-03 * daysPerYear, -6.90460016972063023e-05 * daysPerYear, 9.54791938424326609e-04 * solarMass},
		{8.34336671824457987e+00, 4.12479856412430479e+00, -4.03523417114321381e-01,
			-2.76742510726862411e-03 * daysPerYear, 4.99852801234917238e-03 * daysPerYear, 2.30417297573763929e-05 * daysPerYear, 2.85885980666130812e-04 * solarMass},
		{1.28943695621391310e+01, -1.51111514016986312e+01, -2.23307578892655734e-01,
			2.96460137564761618e-03 * daysPerYear, 2.37847173959480950e-03 * daysPerYear, -2.96589568540237556e-05 * daysPerYear, 4.36624404335156298e-05 * solarMass},
		{1.53796971148509165e+01, -2.59193146099879641e+01, 1.79258772950371181e-01,
			2.68067772490389322e-03 * daysPerYear, 1.62824170038242295e-03 * daysPerYear, -9.51592254519715870e-05 * daysPerYear, 5.15138902046611451e-05 * solarMass},
	}
	var px, py, pz float64
	for i := range b {
		px += b[i].vx * b[i].mass
		py += b[i].vy * b[i].mass
		pz += b[i].vz * b[i].mass
	}
	b[0].vx = -px / solarMass
	b[0].vy = -py / solarMass
	b[0].vz = -pz / solarMass
	// println of floats differs in format only by runtime; scale to ints for a stable checksum.
	println("nbody before", int64(energy(b)*1e9))
	for i := 0; i < steps; i++ {
		advance(b, 0.01)
	}
	println("nbody after", int64(energy(b)*1e9))
}
