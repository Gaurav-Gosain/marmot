package main

type N struct {
	id    int
	peers []*N
}

func main() {
	for round := 0; round < 300; round++ {
		ns := make([]*N, 200)
		for i := range ns {
			ns[i] = &N{id: i}
		}
		for i, n := range ns {
			n.peers = append(n.peers, ns[(i+1)%200], ns[(i+7)%200])
		}
	}
	keep := make([]*N, 100)
	for i := range keep {
		keep[i] = &N{id: i}
	}
	for i, n := range keep {
		n.peers = []*N{keep[(i+1)%100]}
	}
	n := keep[0]
	for i := 0; i < 250; i++ {
		n = n.peers[0]
	}
	println(n.id)
}
