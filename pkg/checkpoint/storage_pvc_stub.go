//go:build !checkpointfull
// +build !checkpointfull

package checkpoint

import "context"

type LocalPVCBackend struct{}

func NewLocalPVCBackend(string, interface{}) (*LocalPVCBackend, error) {
	return nil, errCheckpointDisabled
}
func (*LocalPVCBackend) Save(context.Context, string, SnapshotMeta) error {
	return errCheckpointDisabled
}
func (*LocalPVCBackend) Load(context.Context, SnapshotMeta, string) error {
	return errCheckpointDisabled
}
func (*LocalPVCBackend) List(context.Context, string) ([]SnapshotMeta, error) {
	return nil, errCheckpointDisabled
}
func (*LocalPVCBackend) Delete(context.Context, SnapshotMeta) error { return errCheckpointDisabled }
func (*LocalPVCBackend) Prune(context.Context, string, int) error   { return errCheckpointDisabled }
