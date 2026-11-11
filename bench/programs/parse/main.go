// parse: strconv and strings heavy parsing of key=value records.
package main

import (
	"fmt"
	"strconv"
	"strings"
)

const records = 400000

func main() {
	var sb strings.Builder
	for i := 0; i < records; i++ {
		sb.WriteString("id=")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(";price=")
		sb.WriteString(strconv.FormatFloat(float64(i%1000)*1.25+0.5, 'f', 2, 64))
		sb.WriteString(";name=item")
		sb.WriteString(strconv.Itoa(i % 97))
		sb.WriteString(";ok=")
		sb.WriteString(strconv.FormatBool(i%3 == 0))
		sb.WriteString("\n")
	}
	text := sb.String()
	var idSum int
	var priceSum float64
	names := map[string]int{}
	okCount := 0
	for r := 0; r < 16; r++ {
		for _, line := range strings.Split(text, "\n") {
			if line == "" {
				continue
			}
			for _, f := range strings.Split(line, ";") {
				k, v, found := strings.Cut(f, "=")
				if !found {
					continue
				}
				switch k {
				case "id":
					n, err := strconv.Atoi(v)
					if err == nil {
						idSum += n
					}
				case "price":
					p, err := strconv.ParseFloat(v, 64)
					if err == nil {
						priceSum += p
					}
				case "name":
					names[strings.TrimPrefix(v, "item")]++
				case "ok":
					if b, _ := strconv.ParseBool(v); b {
						okCount++
					}
				}
			}
		}
	}
	fmt.Println(idSum, int64(priceSum*100), len(names), okCount)
}
