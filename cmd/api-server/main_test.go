package main

import "testing"

func TestParseOpts_AcceptsLegacyMetricsAddrFlag(t *testing.T) {
	o, showVersion, err := parseOpts([]string{"--metrics-addr=:8091", "--addr=:8090"})
	if err != nil {
		t.Fatalf("parseOpts returned error: %v", err)
	}
	if showVersion {
		t.Fatalf("showVersion=true, want false")
	}
	if o.metricsAddr != ":8091" {
		t.Fatalf("metricsAddr=%q, want %q", o.metricsAddr, ":8091")
	}
}
