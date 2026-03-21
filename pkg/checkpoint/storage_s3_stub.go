//go:build !s3
// +build !s3

package checkpoint

import (
	"context"
	"errors"
)

var errS3BackendDisabled = errors.New("s3 backend disabled: build with -tags s3 to enable")

// S3Config is retained in non-s3 builds for config compatibility.
type S3Config struct {
	Endpoint        string
	Bucket          string
	Prefix          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

// S3Backend is a non-functional placeholder in default builds.
type S3Backend struct{}

func NewS3Backend(context.Context, S3Config, interface{}) (*S3Backend, error) {
	return nil, errS3BackendDisabled
}

func (*S3Backend) Save(context.Context, string, SnapshotMeta) error          { return errS3BackendDisabled }
func (*S3Backend) Load(context.Context, SnapshotMeta, string) error          { return errS3BackendDisabled }
func (*S3Backend) List(context.Context, string) ([]SnapshotMeta, error)      { return nil, errS3BackendDisabled }
func (*S3Backend) Delete(context.Context, SnapshotMeta) error                { return errS3BackendDisabled }
func (*S3Backend) Prune(context.Context, string, int) error                  { return errS3BackendDisabled }
