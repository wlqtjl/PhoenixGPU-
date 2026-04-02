//go:build !(billingenginefull && billingfull)
// +build !billingenginefull !billingfull

package main

import "fmt"

func main() {
	fmt.Println("billing-engine disabled in default build; use -tags billingenginefull to enable")
}
