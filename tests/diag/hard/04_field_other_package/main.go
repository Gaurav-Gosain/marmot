package main

import (
	"fmt"

	"example.com/app/models"
)

func main() {
	u := models.User{FristName: "Ada", LastName: "Lovelace", Emial: "ada@example.com", Age: 36}
	fmt.Println(u.FirstName, u.Email)
}
