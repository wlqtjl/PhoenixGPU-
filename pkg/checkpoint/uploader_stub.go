//go:build !checkpointfull
// +build !checkpointfull

package checkpoint

import "errors"

type UploaderConfig struct {
	Workers       int
	ChannelBuffer int
	MaxRetries    int
}

type Uploader struct{}

func NewUploader(StorageBackend, UploaderConfig, interface{}) *Uploader { return &Uploader{} }
func (*Uploader) Enqueue(UploadTask) error {
	return errors.New("uploader disabled in non-checkpointfull build")
}
func (*Uploader) Stats() (pending, succeeded, failed int64) { return 0, 0, 0 }
func (*Uploader) Shutdown()                                 {}
