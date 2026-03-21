//go:build !checkpointfull
// +build !checkpointfull

package checkpoint

import (
	"context"
	"errors"
)

var errCheckpointDisabled = errors.New("checkpoint runtime disabled: build with -tags checkpointfull to enable")

type Checkpointer interface {
	Dump(ctx context.Context, pid int, dir string) error
	PreDump(ctx context.Context, pid int, dir string) error
	Restore(ctx context.Context, dir string) (int, error)
	Available() error
}

type CRIUCheckpointer struct{}

func NewCRIUCheckpointer(string, interface{}) (*CRIUCheckpointer, error) {
	return nil, errCheckpointDisabled
}
func (*CRIUCheckpointer) Dump(context.Context, int, string) error    { return errCheckpointDisabled }
func (*CRIUCheckpointer) PreDump(context.Context, int, string) error { return errCheckpointDisabled }
func (*CRIUCheckpointer) Restore(context.Context, string) (int, error) {
	return 0, errCheckpointDisabled
}
func (*CRIUCheckpointer) Available() error { return errCheckpointDisabled }
