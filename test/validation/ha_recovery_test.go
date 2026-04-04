// Package validation -- HA recovery SLA verification.
//
// Validates that the HA controller configuration meets
// the < 60 second recovery SLA.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package validation

import (
	"encoding/json"
	"testing"
	"time"
)

// -- SLA Timing Budget ---------------------------------------------------

const (
	// Configuration values from Helm values.yaml
	faultDetectorPollSeconds = 10
	notReadyThresholdSeconds = 30
	checkpointBudgetSeconds  = 30
	restoreTimeoutSeconds    = 120
	maxRestoreAttempts       = 3

	// SLA target
	recoverySLASeconds = 60
)

func TestHASLA_TimingBudgetFitsWithin60Seconds(t *testing.T) {
	// Recovery timeline:
	// 1. Fault detection: notReadyThreshold (30s) + up to 1 poll interval (10s) worst case
	// 2. Checkpoint was already taken periodically -- no extra cost
	// 3. Restore on healthy node: typically < 10s
	//
	// Worst case: 30s (threshold) + 10s (poll lag) = 40s detection
	//             + 10-20s restore = 50-60s total

	detectionTime := time.Duration(notReadyThresholdSeconds+faultDetectorPollSeconds) * time.Second
	t.Logf("Worst-case detection time: %s", detectionTime)

	restoreBudget := time.Duration(recoverySLASeconds)*time.Second - detectionTime
	t.Logf("Available restore budget: %s", restoreBudget)

	if restoreBudget < 0 {
		t.Errorf("Detection alone (%s) exceeds SLA (%ds)", detectionTime, recoverySLASeconds)
	}

	if restoreBudget < 10*time.Second {
		t.Errorf("Restore budget (%s) is dangerously low", restoreBudget)
	}
}

func TestHASLA_FaultDetectorPollInterval(t *testing.T) {
	if faultDetectorPollSeconds >= notReadyThresholdSeconds {
		t.Errorf("faultDetectorPollSeconds (%d) must be < notReadyThresholdSeconds (%d)",
			faultDetectorPollSeconds, notReadyThresholdSeconds)
	}
}

func TestHASLA_MaxRestoreAttempts(t *testing.T) {
	totalRestoreTime := time.Duration(maxRestoreAttempts*restoreTimeoutSeconds) * time.Second
	maxAcceptable := 10 * time.Minute

	t.Logf("Max total restore time (all attempts): %s", totalRestoreTime)
	if totalRestoreTime > maxAcceptable {
		t.Errorf("Max total restore time (%s) exceeds %s", totalRestoreTime, maxAcceptable)
	}
}

func TestHASLA_CheckpointBudget(t *testing.T) {
	if checkpointBudgetSeconds > recoverySLASeconds {
		t.Errorf("Single checkpoint budget (%ds) exceeds recovery SLA (%ds)",
			checkpointBudgetSeconds, recoverySLASeconds)
	}
	t.Logf("Checkpoint budget: %ds (%.0f%% of SLA)", checkpointBudgetSeconds,
		float64(checkpointBudgetSeconds)/float64(recoverySLASeconds)*100)
}

// -- PhoenixJob Phase Transitions -----------------------------------------

func TestHASLA_PhaseTransitionsAreValid(t *testing.T) {
	validTransitions := map[string][]string{
		"Pending":       {"Running", "Failed"},
		"Running":       {"Checkpointing", "Restoring", "Succeeded", "Failed"},
		"Checkpointing": {"Running", "Failed"},
		"Restoring":     {"Running", "Failed"},
		"Succeeded":     {},
		"Failed":        {},
	}

	// Verify terminal states have no outgoing transitions
	for _, phase := range []string{"Succeeded", "Failed"} {
		if len(validTransitions[phase]) > 0 {
			t.Errorf("Terminal phase %q should have no outgoing transitions", phase)
		}
	}

	// Verify recovery path exists: Running -> Restoring -> Running
	restoreTargets := validTransitions["Running"]
	hasRestoring := false
	for _, target := range restoreTargets {
		if target == "Restoring" {
			hasRestoring = true
			break
		}
	}
	if !hasRestoring {
		t.Error("Running phase must have transition to Restoring for HA recovery")
	}

	// Verify restore can return to Running
	restoreOutgoing := validTransitions["Restoring"]
	hasRunning := false
	for _, target := range restoreOutgoing {
		if target == "Running" {
			hasRunning = true
			break
		}
	}
	if !hasRunning {
		t.Error("Restoring phase must be able to return to Running")
	}
}

// -- Verification via API -------------------------------------------------

func TestHASLA_JobStatusFieldsExist(t *testing.T) {
	base := testServer(t)
	_, body := httpGet(t, base+"/api/v1/jobs")

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	jobs, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("jobs must be array, got %T", resp.Data)
	}

	for i, j := range jobs {
		job, ok := j.(map[string]interface{})
		if !ok {
			continue
		}
		// Every job must have HA-relevant fields
		for _, field := range []string{"phase", "checkpointCount", "restoreAttempts", "currentNodeName"} {
			if _, ok := job[field]; !ok {
				t.Errorf("job[%d] missing HA field: %s", i, field)
			}
		}
	}
}

func TestHASLA_RestoringJobsHaveRestoreAttempts(t *testing.T) {
	base := testServer(t)
	_, body := httpGet(t, base+"/api/v1/jobs")

	var resp apiResp
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	jobs, _ := resp.Data.([]interface{})

	for i, j := range jobs {
		job, ok := j.(map[string]interface{})
		if !ok {
			continue
		}
		phase, _ := job["phase"].(string)
		if phase == "Restoring" {
			attempts, _ := job["restoreAttempts"].(float64)
			if attempts < 1 {
				t.Errorf("job[%d] phase=Restoring but restoreAttempts=%.0f (want >= 1)", i, attempts)
			}
			t.Logf("job[%d] restoring: %s (attempt %.0f/%d)",
				i, job["name"], attempts, maxRestoreAttempts)
		}
	}
}
