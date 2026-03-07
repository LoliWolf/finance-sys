package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"finance-sys/internal/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type ObjectStorage interface {
	EnsureBuckets(ctx context.Context) error
	PutBytes(ctx context.Context, bucket string, objectKey string, contentType string, data []byte) error
	GetBytes(ctx context.Context, bucket string, objectKey string) ([]byte, error)
}

type MinIOStorage struct {
	client *minio.Client
	cfg    config.ObjectStorageConfig
}

func NewMinIOStorage(cfg config.ObjectStorageConfig) (*MinIOStorage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
	})
	if err != nil {
		return nil, fmt.Errorf("new minio client: %w", err)
	}
	return &MinIOStorage{
		client: client,
		cfg:    cfg,
	}, nil
}

func (s *MinIOStorage) EnsureBuckets(ctx context.Context) error {
	buckets := []string{
		s.cfg.BucketDocuments,
		s.cfg.BucketRawMarket,
		s.cfg.BucketReports,
		s.cfg.BucketDeadLetters,
	}
	for _, bucket := range buckets {
		if bucket == "" {
			continue
		}
		exists, err := s.client.BucketExists(ctx, bucket)
		if err != nil {
			return err
		}
		if !exists {
			if err := s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *MinIOStorage) PutBytes(ctx context.Context, bucket string, objectKey string, contentType string, data []byte) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := s.client.PutObject(ctx, bucket, strings.TrimPrefix(objectKey, "/"), bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

func (s *MinIOStorage) GetBytes(ctx context.Context, bucket string, objectKey string) ([]byte, error) {
	object, err := s.client.GetObject(ctx, bucket, strings.TrimPrefix(objectKey, "/"), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	return io.ReadAll(object)
}
