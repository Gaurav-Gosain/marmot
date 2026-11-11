// regexpbench: regexp matching, submatch extraction and replacement over generated log lines.
package main

import (
	"fmt"
	"regexp"
	"strings"
)

const lines = 150000

func main() {
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&sb, "2026-11-%02d 12:%02d:%02d INFO user%d@example.com GET /api/v%d/items/%d 200 %dms\n",
			i%28+1, i%60, (i*7)%60, i%500, i%3+1, i, i%900)
	}
	text := sb.String()
	re := regexp.MustCompile(`(\d+)-(\d+)-(\d+) .* (\w+)@example\.com GET (/\S+) (\d{3}) (\d+)ms`)
	mail := regexp.MustCompile(`user(\d+)@`)
	sum := 0
	for _, line := range strings.Split(text, "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sum += len(m[5]) + len(m[4])
	}
	out := mail.ReplaceAllString(text, "u$1@")
	fmt.Println(sum, len(out), len(mail.FindAllStringIndex(text, -1)))
}
