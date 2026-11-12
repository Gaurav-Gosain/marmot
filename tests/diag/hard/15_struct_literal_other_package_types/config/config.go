package config

import "time"

type Server struct {
	Host    string
	Port    int
	Timeout time.Duration
	Tags    []string
}

func (s Server) Addr() string { return s.Host + ":" + itoa(s.Port) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
