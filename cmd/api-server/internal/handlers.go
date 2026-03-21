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

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const handlerTimeout = 10 * time.Second
const maxHistoryHours = 24 * 30

type handlers struct {
	client K8sClientInterface
	log    *zap.Logger
}

// ── T32: Cluster & Nodes ─────────────────────────────────────────

// GET /api/v1/cluster/summary
func (h *handlers) getClusterSummary(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	summary, err := h.client.GetClusterSummary(ctx)
	if err != nil {
		h.log.Error("getClusterSummary", zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to fetch cluster summary")
		return
	}
	ok(c, summary)
}

// GET /api/v1/cluster/utilization-history?hours=24
func (h *handlers) getUtilHistory(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	hours := 24
	if v := c.Query("hours"); v != "" {
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
		h.log.Error("getUtilHistory", zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to fetch utilization history")
		return
	}
	okMeta(c, pts, len(pts))
}

// GET /api/v1/nodes
func (h *handlers) listNodes(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	nodes, err := h.client.ListGPUNodes(ctx)
	if err != nil {
		h.log.Error("listNodes", zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to list nodes")
		return
	}
	okMeta(c, nodes, len(nodes))
}

// ── T33: Jobs ────────────────────────────────────────────────────

// GET /api/v1/jobs?namespace=<ns>
func (h *handlers) listJobs(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	ns := c.Query("namespace") // empty = all namespaces

	jobs, err := h.client.ListPhoenixJobs(ctx, ns)
	if err != nil {
		h.log.Error("listJobs", zap.String("ns", ns), zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to list jobs")
		return
	}
	okMeta(c, jobs, len(jobs))
}

// GET /api/v1/jobs/:namespace/:name
func (h *handlers) getJob(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	ns := c.Param("namespace")
	name := c.Param("name")

	job, err := h.client.GetPhoenixJob(ctx, ns, name)
	if err != nil {
		if isNotFound(err) {
			errResp(c, http.StatusNotFound,
				"job "+ns+"/"+name+" not found")
			return
		}
		h.log.Error("getJob", zap.String("ns", ns), zap.String("name", name), zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to get job")
		return
	}
	ok(c, job)
}

// POST /api/v1/jobs/:namespace/:name/checkpoint
func (h *handlers) triggerCheckpoint(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	ns := c.Param("namespace")
	name := c.Param("name")

	if err := h.client.TriggerCheckpoint(ctx, ns, name); err != nil {
		if isNotFound(err) {
			errResp(c, http.StatusNotFound, "job "+ns+"/"+name+" not found")
			return
		}
		h.log.Error("triggerCheckpoint",
			zap.String("ns", ns), zap.String("name", name), zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to trigger checkpoint")
		return
	}

	h.log.Info("checkpoint triggered manually",
		zap.String("ns", ns), zap.String("name", name))
	c.JSON(http.StatusAccepted, APIResponse{
		Data: map[string]string{"message": "checkpoint triggered"},
	})
}

// ── T34: Billing ─────────────────────────────────────────────────

// GET /api/v1/billing/departments?period=monthly
func (h *handlers) listBillingDepartments(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	period := c.DefaultQuery("period", "monthly")
	if period != "daily" && period != "weekly" && period != "monthly" {
		errResp(c, http.StatusBadRequest, "period must be daily, weekly, or monthly")
		return
	}

	depts, err := h.client.GetBillingByDepartment(ctx, period)
	if err != nil {
		h.log.Error("listBillingDepartments", zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to fetch billing")
		return
	}
	okMeta(c, depts, len(depts))
}

// GET /api/v1/billing/records?department=<dept>
func (h *handlers) listBillingRecords(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	dept := c.Query("department")

	records, err := h.client.GetBillingRecords(ctx, dept)
	if err != nil {
		h.log.Error("listBillingRecords", zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to fetch records")
		return
	}
	okMeta(c, records, len(records))
}

// ── T35: Alerts ──────────────────────────────────────────────────

// GET /api/v1/alerts
func (h *handlers) listAlerts(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	alerts, err := h.client.ListAlerts(ctx)
	if err != nil {
		h.log.Error("listAlerts", zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to list alerts")
		return
	}
	// Keep response order stable for clients and tests.
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].CreatedAt.After(alerts[j].CreatedAt)
	})

	okMeta(c, alerts, len(alerts))
}

// POST /api/v1/alerts/:id/resolve
func (h *handlers) resolveAlert(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), handlerTimeout)
	defer cancel()

	id := c.Param("id")
	if err := h.client.ResolveAlert(ctx, id); err != nil {
		h.log.Error("resolveAlert", zap.String("id", id), zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to resolve alert")
		return
	}

	h.log.Info("alert resolved", zap.String("id", id))
	ok(c, map[string]string{"message": "alert resolved"})
}
