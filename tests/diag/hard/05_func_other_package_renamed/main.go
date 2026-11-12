package main

import (
	"fmt"

	"example.com/app/textutil"
)

func main() {
	fmt.Println(textutil.FormatName(" Ada", "Lovelace "), textutil.Initial("ada", "lovelace"))
}
