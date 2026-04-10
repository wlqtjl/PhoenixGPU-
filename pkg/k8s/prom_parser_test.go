//go:build k8sfull
// +build k8sfull

// Unit tests for Prometheus HTTP response parsers.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package k8s

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

// makeResponse creates an *http.Response with the given status and body.
func makeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

// ── parsePromScalar ──────────────────────────────────────────────

func TestParsePromScalar_Success(t *testing.T) {
	body := `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{
					"metric": {"instance": "gpu-node-01"},
					"value": [1700000000, "72.5"]
				}
			]
		}
	}`
	v, err := parsePromScalar(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 72.5 {
		t.Errorf("value = %f, want 72.5", v)
	}
}

func TestParsePromScalar_EmptyResult(t *testing.T) {
	body := `{
		"status": "success",
		"data": {"resultType": "vector", "result": []}
	}`
	v, err := parsePromScalar(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 0 {
		t.Errorf("value = %f, want 0 for empty result", v)
	}
}

func TestParsePromScalar_NonOKStatus(t *testing.T) {
	resp := makeResponse(503, "Service Unavailable")
	_, err := parsePromScalar(resp)
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestParsePromScalar_HTTP500(t *testing.T) {
	_, err := parsePromScalar(makeResponse(500, "Internal Server Error"))
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestParsePromScalar_HTTP404(t *testing.T) {
	_, err := parsePromScalar(makeResponse(404, "Not Found"))
	if err == nil {
		t.Fatal("expected error for 404 status")
	}
}

func TestParsePromScalar_InvalidJSON(t *testing.T) {
	_, err := parsePromScalar(makeResponse(200, "not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParsePromScalar_ErrorStatus(t *testing.T) {
	body := `{"status": "error", "errorType": "bad_data", "error": "parse error"}`
	_, err := parsePromScalar(makeResponse(200, body))
	if err == nil {
		t.Fatal("expected error for status=error")
	}
}

func TestParsePromScalar_NonStringValue(t *testing.T) {
	// Value should be a string but we give it a number
	body := `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [{"metric": {}, "value": [1700000000, 42]}]
		}
	}`
	_, err := parsePromScalar(makeResponse(200, body))
	if err == nil {
		t.Fatal("expected error for non-string value in result")
	}
}

func TestParsePromScalar_NonNumericString(t *testing.T) {
	body := `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [{"metric": {}, "value": [1700000000, "NaN"]}]
		}
	}`
	// NaN is actually a valid float — but let's test "not_a_number"
	body2 := `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [{"metric": {}, "value": [1700000000, "not_a_number"]}]
		}
	}`
	_, err := parsePromScalar(makeResponse(200, body2))
	if err == nil {
		t.Fatal("expected error for non-numeric string value")
	}

	// "NaN" is parsed by ParseFloat — verify it doesn't error
	_, err = parsePromScalar(makeResponse(200, body))
	if err != nil {
		t.Fatalf("NaN should be parseable: %v", err)
	}
}

func TestParsePromScalar_MultipleResults_UsesFirst(t *testing.T) {
	body := `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [
				{"metric": {"instance": "a"}, "value": [1700000000, "10"]},
				{"metric": {"instance": "b"}, "value": [1700000000, "20"]}
			]
		}
	}`
	v, err := parsePromScalar(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 10 {
		t.Errorf("value = %f, want 10 (should use first result)", v)
	}
}

func TestParsePromScalar_ZeroValue(t *testing.T) {
	body := `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [{"metric": {}, "value": [1700000000, "0"]}]
		}
	}`
	v, err := parsePromScalar(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 0 {
		t.Errorf("value = %f, want 0", v)
	}
}

func TestParsePromScalar_NegativeValue(t *testing.T) {
	body := `{
		"status": "success",
		"data": {
			"resultType": "vector",
			"result": [{"metric": {}, "value": [1700000000, "-5.5"]}]
		}
	}`
	v, err := parsePromScalar(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != -5.5 {
		t.Errorf("value = %f, want -5.5", v)
	}
}

// ── parsePromRange ───────────────────────────────────────────────

func TestParsePromRange_Success(t *testing.T) {
	body := `{
		"status": "success",
		"data": {
			"resultType": "matrix",
			"result": [{
				"metric": {"__name__": "gpu_util"},
				"values": [
					[1700000000, "60.5"],
					[1700000060, "65.2"],
					[1700000120, "70.0"]
				]
			}]
		}
	}`
	pts, err := parsePromRange(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pts) != 3 {
		t.Fatalf("expected 3 points, got %d", len(pts))
	}
	if pts[0].Value != 60.5 {
		t.Errorf("first point value = %f, want 60.5", pts[0].Value)
	}
	if pts[2].Value != 70.0 {
		t.Errorf("last point value = %f, want 70.0", pts[2].Value)
	}
	// Verify timestamps are set
	if pts[0].TS.IsZero() {
		t.Error("first point has zero timestamp")
	}
}

func TestParsePromRange_EmptyResult(t *testing.T) {
	body := `{
		"status": "success",
		"data": {"resultType": "matrix", "result": []}
	}`
	pts, err := parsePromRange(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pts != nil {
		t.Errorf("expected nil for empty range result, got %d points", len(pts))
	}
}

func TestParsePromRange_NonOKStatus(t *testing.T) {
	_, err := parsePromRange(makeResponse(500, "error"))
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestParsePromRange_InvalidJSON(t *testing.T) {
	_, err := parsePromRange(makeResponse(200, "{invalid"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParsePromRange_ErrorStatus(t *testing.T) {
	body := `{"status": "error", "errorType": "timeout", "error": "query timeout"}`
	_, err := parsePromRange(makeResponse(200, body))
	if err == nil {
		t.Fatal("expected error for status=error")
	}
}

func TestParsePromRange_SkipsMalformedSamples(t *testing.T) {
	// Mix of valid and invalid samples — invalid should be skipped
	body := `{
		"status": "success",
		"data": {
			"resultType": "matrix",
			"result": [{
				"metric": {},
				"values": [
					[1700000000, "50.0"],
					["not_a_timestamp", "60.0"],
					[1700000120, 70],
					[1700000180, "not_a_number"],
					[1700000240, "80.0"]
				]
			}]
		}
	}`
	pts, err := parsePromRange(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the first and last valid samples should parse:
	// [1700000000, "50.0"] and [1700000240, "80.0"]
	if len(pts) != 2 {
		t.Errorf("expected 2 valid points (skipping malformed), got %d", len(pts))
	}
}

func TestParsePromRange_EmptyValues(t *testing.T) {
	body := `{
		"status": "success",
		"data": {
			"resultType": "matrix",
			"result": [{
				"metric": {},
				"values": []
			}]
		}
	}`
	pts, err := parsePromRange(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pts) != 0 {
		t.Errorf("expected 0 points for empty values, got %d", len(pts))
	}
}

func TestParsePromRange_MultipleResults_UsesFirst(t *testing.T) {
	body := `{
		"status": "success",
		"data": {
			"resultType": "matrix",
			"result": [
				{
					"metric": {"instance": "a"},
					"values": [[1700000000, "10"]]
				},
				{
					"metric": {"instance": "b"},
					"values": [[1700000000, "20"], [1700000060, "30"]]
				}
			]
		}
	}`
	pts, err := parsePromRange(makeResponse(200, body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should only use first result series
	if len(pts) != 1 {
		t.Errorf("expected 1 point from first result, got %d", len(pts))
	}
	if pts[0].Value != 10 {
		t.Errorf("value = %f, want 10", pts[0].Value)
	}
}

// ── NewFakeClient ────────────────────────────────────────────────

func TestNewFakeClient_ReturnsInterface(t *testing.T) {
	c := NewFakeClient()
	if c == nil {
		t.Fatal("NewFakeClient returned nil")
	}
}
