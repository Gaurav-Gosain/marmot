package main

type Store interface {
	Get(key string) (string, bool)
	Put(key, value string)
	Delete(key string)
}
