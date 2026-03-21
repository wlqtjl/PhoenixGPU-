//go:build !controllerfull
// +build !controllerfull

package main

import "fmt"

func main() {
	fmt.Println("phoenix-controller disabled in default build; use -tags controllerfull to enable")
}
