package market

import (
	"context"
	"fmt"
	"time"

	"finance-sys/internal/storage"
)

type StorageArchiver struct {
	store  storage.ObjectStorage
	bucket string
}

func NewStorageArchiver(store storage.ObjectStorage, bucket string) *StorageArchiver {
	return &StorageArchiver{
		store:  store,
		bucket: bucket,
	}
}

func (a *StorageArchiver) Archive(ctx context.Context, provider string, kind string, payload []byte) (string, error) {
	if a.store == nil || a.bucket == "" || len(payload) == 0 {
		return "", nil
	}
	now := time.Now().UTC()
	objectKey := fmt.Sprintf("%s/%04d/%02d/%02d/%d.json", provider, now.Year(), now.Month(), now.Day(), now.UnixNano())
	if err := a.store.PutBytes(ctx, a.bucket, objectKey, "application/json", payload); err != nil {
		return "", err
	}
	return objectKey, nil
}
