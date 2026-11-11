package main

import (
	"bufio"
	"strings"
)

func main() {
	words := bufio.NewScanner(strings.NewReader("  the quick\tbrown\n fox  "))
	words.Split(bufio.ScanWords)
	for words.Scan() {
		print("[", words.Text(), "]")
	}
	println()
	runes := bufio.NewScanner(strings.NewReader("aé世"))
	runes.Split(bufio.ScanRunes)
	for runes.Scan() {
		print(len(runes.Text()), " ")
	}
	println()
	bs := bufio.NewScanner(strings.NewReader("ab"))
	bs.Split(bufio.ScanBytes)
	for bs.Scan() {
		print(bs.Text(), ".")
	}
	println()
	custom := bufio.NewScanner(strings.NewReader("a,b,,c"))
	custom.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		for i, c := range data {
			if c == ',' {
				return i + 1, data[:i], nil
			}
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for custom.Scan() {
		print("<", custom.Text(), ">")
	}
	println()
}
