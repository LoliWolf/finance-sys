package storage

import "context"

type NopStorage struct{}

func (NopStorage) EnsureBuckets(context.Context) error { return nil }
func (NopStorage) PutBytes(context.Context, string, string, string, []byte) error {
	return nil
}
func (NopStorage) GetBytes(context.Context, string, string) ([]byte, error) {
	return nil, nil
}
