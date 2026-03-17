// Package k8s — Prometheus HTTP response parsers.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package k8s

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	apitypes "github.com/wlqtjl/PhoenixGPU/cmd/api-server/internal"
)

// promInstantResponse is the JSON shape of a Prometheus instant query result.
type promInstantResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]interface{}    `json:"value"` // [timestamp, value_string]
		} `json:"result"`
	} `json:"data"`
}

// promRangeResponse is the JSON shape of a Prometheus range query result.
type promRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]interface{}  `json:"values"` // [[timestamp, value_string], ...]
		} `json:"result"`
	} `json:"data"`
}

// parsePromScalar reads an HTTP response from a Prometheus instant query
// and returns the first scalar value.
func parsePromScalar(resp *http.Response) (float64, error) {
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MiB limit
	if err != nil {
		return 0, fmt.Errorf("read prom response: %w", err)
	}

	var pr promInstantResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return 0, fmt.Errorf("parse prom response: %w", err)
	}
	if pr.Status != "success" {
		return 0, fmt.Errorf("prometheus query failed: status=%s", pr.Status)
	}
	if len(pr.Data.Result) == 0 {
		return 0, nil // no data — return 0, not an error
	}

	valStr, ok := pr.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected value type in prom response")
	}
	v, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse prom value %q: %w", valStr, err)
	}
	return v, nil
}

// parsePromRange reads an HTTP response from a Prometheus range query
// and returns a time series.
func parsePromRange(resp *http.Response) ([]apitypes.TimeSeriesPoint, error) {
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus range query returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4MiB limit
	if err != nil {
		return nil, fmt.Errorf("read prom range response: %w", err)
	}

	var pr promRangeResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("parse prom range response: %w", err)
	}
	if pr.Status != "success" {
		return nil, fmt.Errorf("prometheus range query failed: %s", pr.Status)
	}
	if len(pr.Data.Result) == 0 {
		return nil, nil
	}

	var points []apitypes.TimeSeriesPoint
	for _, sample := range pr.Data.Result[0].Values {
		tsFloat, ok := sample[0].(float64)
		if !ok {
			continue
		}
		valStr, ok := sample[1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		points = append(points, apitypes.TimeSeriesPoint{
			TS:    time.Unix(int64(tsFloat), 0).UTC(),
			Value: v,
		})
	}
	return points, nil
}

// ── FakeClient wrapper for test access ────────────────────────────

// NewFakeClient returns a K8sClientInterface backed by fake data.
// Used in tests and when running without a real K8s cluster.
func NewFakeClient() apitypes.K8sClientInterface {
	return apitypes.NewFakeK8sClient()
}
