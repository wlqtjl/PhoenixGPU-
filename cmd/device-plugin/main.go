package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	addr := flag.String("addr", ":9800", "health listen address")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("%s (%s, %s)\n", version, commit, date)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	log.Printf("device-plugin bootstrap listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
