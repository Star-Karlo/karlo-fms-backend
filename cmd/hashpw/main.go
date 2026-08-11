// Command hashpw prints a bcrypt hash (cost 12, matching the app) for a password
// you pass on the command line — for a manual DB password reset. Throwaway.
//
//	go run ./cmd/hashpw 'YourNewPassword'
package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: go run ./cmd/hashpw '<password>'")
		os.Exit(1)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(os.Args[1]), 12)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(h))
}
