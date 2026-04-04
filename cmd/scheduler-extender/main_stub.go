//go:build !schedulerfull
// +build !schedulerfull

package main

import "fmt"

func main() {
	fmt.Println("scheduler-extender disabled in default build; use -tags schedulerfull to enable")
}
