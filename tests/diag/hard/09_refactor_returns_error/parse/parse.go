package parse

import (
	"errors"
	"strconv"
	"strings"
)

// Fields parses "a=1,b=2" into a map. It now reports malformed input.
func Fields(s string) (map[string]int, error) {
	m := map[string]int{}
	for _, kv := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, errors.New("missing =")
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, err
		}
		m[k] = n
	}
	return m, nil
}
