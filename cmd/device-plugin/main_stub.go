//go:build !devicepluginfull
// +build !devicepluginfull

package main

import "fmt"

func main() {
	fmt.Println("device-plugin disabled in default build; use -tags devicepluginfull to enable")
}
