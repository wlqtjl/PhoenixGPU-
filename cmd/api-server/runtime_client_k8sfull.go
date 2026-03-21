//go:build k8sfull
// +build k8sfull

package main

import (
	"github.com/wlqtjl/PhoenixGPU/cmd/api-server/internal"
	pkgk8s "github.com/wlqtjl/PhoenixGPU/pkg/k8s"
	"go.uber.org/zap"
)

func buildK8sClient(o *opts) (internal.K8sClientInterface, error) {
	if o.mock {
		return internal.NewFakeK8sClient(), nil
	}
	logger := zap.NewNop()
	return pkgk8s.NewRealK8sClient(o.promURL, logger)
}
