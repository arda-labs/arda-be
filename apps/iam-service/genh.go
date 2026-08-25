package main

import (
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"os"
	"strings"
)

func main() {
	value := strings.TrimSpace(os.Getenv("PASSWORD_TO_HASH"))
	if value == "" {
		panic("PASSWORD_TO_HASH must be provided through the environment")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(h))
}
