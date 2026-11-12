package main

type Mem struct{ m map[string]string }

func NewMem() *Mem { return &Mem{m: map[string]string{}} }

func (s *Mem) Get(key string) (string, bool) { v, ok := s.m[key]; return v, ok }
func (s *Mem) Set(key, value string)          { s.m[key] = value }
