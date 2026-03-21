// Package internal — HTTP handlers for the PhoenixGPU API.
//
// Each handler:
//  1. Reads request context (with timeout)
//  2. Calls K8sClient to fetch data
//  3. Returns unified APIResponse envelope
//  4. Logs result via structured zap
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package internal

import (
	"context"
	"net/http"
	"sort"
	"time"
)

const handlerTimeout = 10 * time.Second
const maxHistoryHours = 24 * 30

type handlers struct {
	client K8sClientInterface
	log    Logger
}

func (h *handlers) getClusterSummary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	summary, err := h.client.GetClusterSummary(ctx)
	if err != nil {
		h.log.Error("getClusterSummary", "err", err)
		errResp(w, http.StatusInternalServerError, "failed to fetch cluster summary")
		return
	}
	ok(w, summary)
}

func (h *handlers) getUtilHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if n, err := parsePositiveInt(v); err == nil {
			// Upper bound prevents unbounded response sizes and memory pressure
			// caused by accidental requests such as ?hours=999999.
			if n > maxHistoryHours {
				n = maxHistoryHours
			}
			hours = n
		}
	}

	pts, err := h.client.GetUtilizationHistory(ctx, hours)
	if err != nil {
		h.log.Error("getUtilHistory", "err", err)
		errResp(w, http.StatusInternalServerError, "failed to fetch utilization history")
		return
	}
	okMeta(w, pts, len(pts))
}

func (h *handlers) listNodes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	nodes, err := h.client.ListGPUNodes(ctx)
	if err != nil {
		h.log.Error("listNodes", "err", err)
		errResp(w, http.StatusInternalServerError, "failed to list nodes")
		return
	}
	okMeta(w, nodes, len(nodes))
}

func (h *handlers) listJobs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	ns := r.URL.Query().Get("namespace")
	jobs, err := h.client.ListPhoenixJobs(ctx, ns)
	if err != nil {
		h.log.Error("listJobs", "ns", ns, "err", err)
		errResp(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}
	okMeta(w, jobs, len(jobs))
}

func (h *handlers) getJob(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	ns := c.Param("namespace")
	name := c.Param("name")

	job, err := h.client.GetPhoenixJob(ctx, ns, name)
	if err != nil {
		if isNotFound(err) {
			errResp(w, http.StatusNotFound, "job "+ns+"/"+name+" not found")
			return
		}
		h.log.Error("getJob", "ns", ns, "name", name, "err", err)
		errResp(w, http.StatusInternalServerError, "failed to get job")
		return
	}
	ok(w, job)
}

func (h *handlers) triggerCheckpoint(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	ns := c.Param("namespace")
	name := c.Param("name")

	if err := h.client.TriggerCheckpoint(ctx, ns, name); err != nil {
		if isNotFound(err) {
			errResp(w, http.StatusNotFound, "job "+ns+"/"+name+" not found")
			return
		}
		h.log.Error("triggerCheckpoint", "ns", ns, "name", name, "err", err)
		errResp(w, http.StatusInternalServerError, "failed to trigger checkpoint")
		return
	}

	jsonResponse(w, http.StatusAccepted, APIResponse{Data: map[string]string{"message": "checkpoint triggered"}})
}

func (h *handlers) listBillingDepartments(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "monthly"
	}
	if period != "daily" && period != "weekly" && period != "monthly" {
		errResp(w, http.StatusBadRequest, "period must be daily, weekly, or monthly")
		return
	}

	depts, err := h.client.GetBillingByDepartment(ctx, period)
	if err != nil {
		h.log.Error("listBillingDepartments", "err", err)
		errResp(w, http.StatusInternalServerError, "failed to fetch billing")
		return
	}
	okMeta(w, depts, len(depts))
}

func (h *handlers) listBillingRecords(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	records, err := h.client.GetBillingRecords(ctx, r.URL.Query().Get("department"))
	if err != nil {
		h.log.Error("listBillingRecords", "err", err)
		errResp(w, http.StatusInternalServerError, "failed to fetch records")
		return
	}
	okMeta(w, records, len(records))
}

func (h *handlers) listAlerts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	alerts, err := h.client.ListAlerts(ctx)
	if err != nil {
		h.log.Error("listAlerts", "err", err)
		errResp(w, http.StatusInternalServerError, "failed to list alerts")
		return
	}
	// Keep response order stable for clients and tests.
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].CreatedAt.After(alerts[j].CreatedAt)
	})

	okMeta(c, alerts, len(alerts))
}

func (h *handlers) resolveAlert(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()

	id, okPath := parseResolveAlertPath(r.URL.Path)
	if !okPath {
		errResp(w, http.StatusBadRequest, "invalid alert path")
		return
	}
	if err := h.client.ResolveAlert(ctx, id); err != nil {
		h.log.Error("resolveAlert", "id", id, "err", err)
		errResp(w, http.StatusInternalServerError, "failed to resolve alert")
		return
	}
	ok(w, map[string]string{"message": "alert resolved"})
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

func parseJobPath(path string) (namespace, name string, ok bool) {
	parts := splitPath(path)
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "jobs" {
		return parts[3], parts[4], true
	}
	return "", "", false
}

func parseCheckpointPath(path string) (namespace, name string, ok bool) {
	parts := splitPath(path)
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "jobs" && parts[5] == "checkpoint" {
		return parts[3], parts[4], true
	}
	return "", "", false
}

func parseResolveAlertPath(path string) (id string, ok bool) {
	parts := splitPath(path)
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "alerts" && parts[4] == "resolve" {
		return parts[3], true
	}
	return "", false
}
