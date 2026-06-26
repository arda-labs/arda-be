package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: go run main.go <password_to_check>")
		os.Exit(1)
	}

	h := []byte("$2a$12$LJ3m4ys3Lk0TSwHlvS.JJOvc5sx5GQJfKPdKR0MJfN.ZcJKW5K7iW")
	password := os.Args[1]

	err := bcrypt.CompareHashAndPassword(h, []byte(password))
	if err != nil {
		fmt.Printf("❌ WRONG! '%s' does NOT match the hash. Error: %v\n", password, err)
	} else {
		fmt.Printf("✅ CORRECT! '%s' matches the hash.\n", password)
	}
}
