//go:build migration
// +build migration

// Migration API handler — POST /api/v1/jobs/:ns/:name/migrate
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package internal

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/wlqtjl/PhoenixGPU/pkg/migration"
)

// MigrationExecutor is injected into the router for live migration requests.
type MigrationExecutor interface {
	Execute(ctx context.Context, plan migration.Plan) (*migration.Result, error)
}

// registerMigrationRoutes adds migration endpoints to an existing router group.
func registerMigrationRoutes(v1 *gin.RouterGroup, executor MigrationExecutor, log *zap.Logger) {
	mh := &migrationHandlers{executor: executor, log: log}
	v1.POST("/jobs/:namespace/:name/migrate", mh.triggerMigration)
	v1.GET("/jobs/:namespace/:name/migration-status", mh.getMigrationStatus)
}

type migrationHandlers struct {
	executor MigrationExecutor
	log      *zap.Logger
	// In-memory status store (Sprint 8: replace with K8s annotation)
	status map[string]*migration.Result
}

// POST /api/v1/jobs/:namespace/:name/migrate
// Body: { "targetNode": "gpu-node-02" }
func (h *migrationHandlers) triggerMigration(c *gin.Context) {
	ns := c.Param("namespace")
	name := c.Param("name")

	var req migration.MigrateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest,
			"invalid request body: "+err.Error())
		return
	}
	req.JobNamespace = ns
	req.JobName = name

	if err := req.Validate(); err != nil {
		errResp(c, http.StatusBadRequest, err.Error())
		return
	}

	h.log.Info("live migration triggered",
		zap.String("job", ns+"/"+name),
		zap.String("target", req.TargetNode))

	// Migration is async — return 202 Accepted immediately
	// The actual migration runs in a goroutine
	go func() {
		plan := migration.Plan{
			JobNamespace: req.JobNamespace,
			JobName:      req.JobName,
			TargetNode:   req.TargetNode,
		}
		result, err := h.executor.Execute(context.Background(), plan)
		if err != nil {
			h.log.Error("live migration failed",
				zap.String("job", ns+"/"+name),
				zap.Error(err))
			return
		}
		h.log.Info("live migration completed",
			zap.String("job", ns+"/"+name),
			zap.Duration("total", result.TotalDuration),
			zap.Duration("freeze", result.FreezeWindow))
	}()

	c.JSON(http.StatusAccepted, APIResponse{
		Data: map[string]string{
			"message":    "migration started",
			"job":        ns + "/" + name,
			"targetNode": req.TargetNode,
		},
	})
}

// GET /api/v1/jobs/:namespace/:name/migration-status
func (h *migrationHandlers) getMigrationStatus(c *gin.Context) {
	ns := c.Param("namespace")
	name := c.Param("name")
	// Placeholder: real implementation reads from K8s annotation
	ok(c, map[string]string{
		"job":    ns + "/" + name,
		"status": "no_migration_in_progress",
	})
}
