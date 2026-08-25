package main

import (
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"os"
	"strings"
)

func main() {
	value := strings.TrimSpace(os.Getenv("PASSWORD_TO_VERIFY"))
	if value == "" {
		panic("PASSWORD_TO_VERIFY must be provided through the environment")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	fmt.Println("NEW_HASH:" + string(hash))

	// Also verify the existing hash
	oldHash := []byte("$2a$12$LJ3m4ys3Lk0TSwHlvS.JJOvc5sx5GQJfKPdKR0MJfN.ZcJKW5K7iW")
	err = bcrypt.CompareHashAndPassword(oldHash, []byte(value))
	if err != nil {
		fmt.Println("OLD_HASH_MISMATCH:" + err.Error())
	} else {
		fmt.Println("OLD_HASH_MATCHES_PROVIDED_VALUE")
	}
}
