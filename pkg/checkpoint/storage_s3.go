//go:build s3
// +build s3

// Package checkpoint — S3 StorageBackend.
//
// Implements zero-disk-copy upload using io.Pipe + S3 Multipart Upload.
// Files stream directly from the local filesystem to S3 without a second
// on-disk buffer — satisfying Engineering Covenant §6 "零磁盘二次拷贝".
//
// S3 key layout:
//   <prefix>/<namespace>/<jobName>/ckpt-<seq:05d>/<filename>
//   <prefix>/<namespace>/<jobName>/ckpt-<seq:05d>/meta.json
//
// Copyright 2025 PhoenixGPU Authors
// SPDX-License-Identifier: Apache-2.0
package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// S3Config holds connection parameters for an S3-compatible store.
type S3Config struct {
	Endpoint        string // empty = AWS; non-empty = MinIO/Ceph/etc.
	Bucket          string
	Prefix          string // optional key prefix
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool // required for MinIO
}

// S3Backend implements StorageBackend backed by S3-compatible object storage.
type S3Backend struct {
	client *s3.Client
	cfg    S3Config
	logger *zap.Logger

	// Prometheus metrics (Engineering Covenant §4)
	uploadBytes    *prometheus.CounterVec
	uploadDuration *prometheus.HistogramVec
	uploadFailures *prometheus.CounterVec
}

// Prometheus metrics — registered once per process.
var (
	s3MetricsOnce     sync.Once
	s3UploadBytes     *prometheus.CounterVec
	s3UploadDuration  *prometheus.HistogramVec
	s3UploadFailures  *prometheus.CounterVec
)

func initS3Metrics() {
	s3MetricsOnce.Do(func() {
		s3UploadBytes = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "phoenixgpu_snapshot_upload_bytes_total",
			Help: "Total bytes uploaded to S3 snapshot storage",
		}, []string{"namespace", "job"})

		s3UploadDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "phoenixgpu_snapshot_upload_duration_seconds",
			Help:    "Time taken to upload a snapshot file to S3",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 300},
		}, []string{"namespace", "job", "result"})

		s3UploadFailures = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "phoenixgpu_snapshot_upload_failures_total",
			Help: "Total S3 upload failures by reason",
		}, []string{"namespace", "job", "reason"})
	})
}

// NewS3Backend constructs an S3Backend and validates connectivity.
func NewS3Backend(ctx context.Context, cfg S3Config, logger *zap.Logger) (*S3Backend, error) {
	initS3Metrics()

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("s3 config: %w", err)
	}

	opts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = cfg.ForcePathStyle
		})
	}

	client := s3.NewFromConfig(awsCfg, opts...)
	return &S3Backend{
		client:         client,
		cfg:            cfg,
		logger:         logger,
		uploadBytes:    s3UploadBytes,
		uploadDuration: s3UploadDuration,
		uploadFailures: s3UploadFailures,
	}, nil
}

// ── Save ──────────────────────────────────────────────────────────
// Uploads all files in src to S3 using io.Pipe (zero disk copy).

func (b *S3Backend) Save(ctx context.Context, src string, meta SnapshotMeta) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("s3 save readdir %s: %w", src, err)
	}

	var totalBytes int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("s3 save cancelled: %w", ctx.Err())
		default:
		}

		localPath := filepath.Join(src, entry.Name())
		key := b.objectKey(meta, entry.Name())

		n, err := b.uploadFile(ctx, localPath, key, meta)
		if err != nil {
			b.uploadFailures.WithLabelValues(meta.Namespace, meta.JobName, "upload").Inc()
			return fmt.Errorf("s3 upload %s: %w", entry.Name(), err)
		}
		totalBytes += n
		b.uploadBytes.WithLabelValues(meta.Namespace, meta.JobName).Add(float64(n))
	}

	// Upload meta.json last (signals snapshot is complete)
	meta.SizeBytes = totalBytes
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	metaKey := b.objectKey(meta, "meta.json")
	if err := b.putBytes(ctx, metaKey, metaData); err != nil {
		return fmt.Errorf("s3 upload meta.json: %w", err)
	}

	b.logger.Info("s3 snapshot saved",
		zap.String("job", meta.JobKey()),
		zap.Int("seq", meta.Seq),
		zap.String("bucket", b.cfg.Bucket),
		zap.Int64("bytes", totalBytes))
	return nil
}

// uploadFile streams a file to S3 using io.Pipe — no intermediate buffer.
// Caller gets: file → Reader end of Pipe → S3 PutObject.
func (b *S3Backend) uploadFile(
	ctx context.Context, localPath, key string, meta SnapshotMeta,
) (int64, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", localPath, err)
	}
	defer f.Close()

	info, _ := f.Stat()
	size := info.Size()

	// io.Pipe: file reader → S3 writer with zero intermediate buffering
	pr, pw := io.Pipe()

	// Goroutine: copy file into the write-end of the pipe
	var copyErr error
	go func() {
		_, copyErr = io.Copy(pw, f)
		pw.CloseWithError(copyErr) // signals EOF or error to the reader end
	}()

	timer := prometheus.NewTimer(b.uploadDuration.WithLabelValues(
		meta.Namespace, meta.JobName, "success"))
	defer timer.ObserveDuration()

	_, putErr := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.cfg.Bucket),
		Key:           aws.String(key),
		Body:          pr,  // S3 SDK reads from the pipe
		ContentLength: aws.Int64(size),
	})
	pr.Close()

	if putErr != nil {
		b.uploadDuration.WithLabelValues(meta.Namespace, meta.JobName, "failure")
		return 0, fmt.Errorf("s3 PutObject %s: %w", key, putErr)
	}
	if copyErr != nil {
		return 0, fmt.Errorf("copy to pipe %s: %w", localPath, copyErr)
	}
	return size, nil
}

// ── Load ──────────────────────────────────────────────────────────

func (b *S3Backend) Load(ctx context.Context, meta SnapshotMeta, dst string) error {
	// First fetch meta.json to discover which files to download
	metaKey := b.objectKey(meta, "meta.json")
	metaData, err := b.getBytes(ctx, metaKey)
	if err != nil {
		return fmt.Errorf("snapshot not found (ns=%s job=%s seq=%d): %w",
			meta.Namespace, meta.JobName, meta.Seq, err)
	}

	var savedMeta SnapshotMeta
	if err := json.Unmarshal(metaData, &savedMeta); err != nil {
		return fmt.Errorf("parse meta.json: %w", err)
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return fmt.Errorf("s3 load mkdir %s: %w", dst, err)
	}

	// List objects under this snapshot prefix
	prefix := b.snapPrefix(meta)
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.cfg.Bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("s3 list %s: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			filename := filepath.Base(key)
			if filename == "meta.json" {
				continue
			}
			if err := b.downloadFile(ctx, key, filepath.Join(dst, filename)); err != nil {
				return fmt.Errorf("s3 download %s: %w", filename, err)
			}
		}
	}

	b.logger.Info("s3 snapshot loaded",
		zap.String("job", meta.JobKey()),
		zap.Int("seq", meta.Seq),
		zap.String("dst", dst))
	return nil
}

func (b *S3Backend) downloadFile(ctx context.Context, key, dst string) error {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("GetObject %s: %w", key, err)
	}
	defer out.Body.Close()

	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, out.Body); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return f.Sync()
}

// ── List ──────────────────────────────────────────────────────────

func (b *S3Backend) List(ctx context.Context, jobKey string) ([]SnapshotMeta, error) {
	parts := strings.SplitN(jobKey, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid jobKey %q", jobKey)
	}
	prefix := strings.TrimRight(b.cfg.Prefix, "/") + "/" + parts[0] + "/" + parts[1] + "/"

	var metas []SnapshotMeta
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket:    aws.String(b.cfg.Bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list %s: %w", prefix, err)
		}
		for _, cp := range page.CommonPrefixes {
			// Each common prefix is a ckpt-XXXXX/ directory
			metaKey := aws.ToString(cp.Prefix) + "meta.json"
			data, err := b.getBytes(ctx, metaKey)
			if err != nil {
				b.logger.Warn("skip unreadable snapshot meta", zap.String("key", metaKey))
				continue
			}
			var m SnapshotMeta
			if err := json.Unmarshal(data, &m); err != nil {
				continue
			}
			metas = append(metas, m)
		}
	}

	sort.Slice(metas, func(i, j int) bool { return metas[i].Seq < metas[j].Seq })
	return metas, nil
}

// ── Delete ────────────────────────────────────────────────────────

func (b *S3Backend) Delete(ctx context.Context, meta SnapshotMeta) error {
	prefix := b.snapPrefix(meta)
	paginator := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.cfg.Bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("s3 delete list %s: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			if _, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(b.cfg.Bucket),
				Key:    obj.Key,
			}); err != nil {
				return fmt.Errorf("s3 delete %s: %w", aws.ToString(obj.Key), err)
			}
		}
	}
	return nil
}

// ── Prune ─────────────────────────────────────────────────────────

func (b *S3Backend) Prune(ctx context.Context, jobKey string, keep int) error {
	metas, err := b.List(ctx, jobKey)
	if err != nil {
		return fmt.Errorf("s3 prune list: %w", err)
	}
	if len(metas) <= keep {
		return nil
	}
	for _, m := range metas[:len(metas)-keep] {
		if err := b.Delete(ctx, m); err != nil {
			return fmt.Errorf("s3 prune delete seq=%d: %w", m.Seq, err)
		}
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────

func (b *S3Backend) objectKey(meta SnapshotMeta, filename string) string {
	return b.snapPrefix(meta) + filename
}

func (b *S3Backend) snapPrefix(meta SnapshotMeta) string {
	prefix := strings.TrimRight(b.cfg.Prefix, "/")
	return fmt.Sprintf("%s/%s/%s/ckpt-%05d/", prefix, meta.Namespace, meta.JobName, meta.Seq)
}

func (b *S3Backend) putBytes(ctx context.Context, key string, data []byte) error {
	pr, pw := io.Pipe()
	go func() {
		_, err := pw.Write(data)
		pw.CloseWithError(err)
	}()
	size := int64(len(data))
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.cfg.Bucket),
		Key:           aws.String(key),
		Body:          pr,
		ContentLength: aws.Int64(size),
	})
	pr.Close()
	if err != nil {
		return fmt.Errorf("s3 PutObject bytes %s: %w", key, err)
	}
	return nil
}

func (b *S3Backend) getBytes(ctx context.Context, key string) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 GetObject %s: %w", key, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("s3 read body %s: %w", key, err)
	}
	return data, nil
}
