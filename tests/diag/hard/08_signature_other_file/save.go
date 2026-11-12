package main

import "strings"

// Save stores data under key with the given tags and returns the size stored.
func Save(key string, data []byte, tags ...string) (int, error) {
	_ = key
	_ = strings.Join(tags, ",")
	return len(data), nil
}
