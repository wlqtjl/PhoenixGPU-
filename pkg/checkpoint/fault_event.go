// Package checkpoint — exported types for e2e test access.
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package checkpoint

import "time"

// FaultEvent is exported here so e2e tests can reference it without
// importing hacontroller (avoids circular dependency).
type FaultEvent struct {
	NodeName   string
	DetectedAt time.Time
}
