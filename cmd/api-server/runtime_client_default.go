//go:build !k8sfull
// +build !k8sfull

package main

import (
	"fmt"

	"github.com/wlqtjl/PhoenixGPU/cmd/api-server/internal"
)

func buildK8sClient(o *opts) (internal.K8sClientInterface, error) {
	if o.mock {
		return internal.NewFakeK8sClient(), nil
	}
	return nil, fmt.Errorf("real K8s client unavailable in default build: recompile with -tags k8sfull or run with --mock")
}
