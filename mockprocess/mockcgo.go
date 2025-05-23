// package main provides a mock CGO executable for testing
package main

import "fmt"

func main() {
	port := 8080
	//nolint:forbidigo
	fmt.Println("Server successfully started:", port)
	//nolint:forbidigo
	fmt.Println("done setting up gateway")
}
