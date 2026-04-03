//go:build !webhookfull
// +build !webhookfull

package main

import "fmt"

func main() {
	fmt.Println("webhook disabled in default build; use -tags webhookfull to enable")
}
