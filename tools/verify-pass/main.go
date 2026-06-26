package main

import (
	"fmt"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	fmt.Println("NEW_HASH:" + string(hash))

	// Also verify the existing hash
	oldHash := []byte("$2a$12$LJ3m4ys3Lk0TSwHlvS.JJOvc5sx5GQJfKPdKR0MJfN.ZcJKW5K7iW")
	err = bcrypt.CompareHashAndPassword(oldHash, []byte("admin123"))
	if err != nil {
		fmt.Println("OLD_HASH_MISMATCH:" + err.Error())
	} else {
		fmt.Println("OLD_HASH_MATCHES_admin123")
	}
}
