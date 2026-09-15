package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cloud.google.com/go/storage"
)

// Provider defines the interface for object storage interactions.
type Provider interface {
	Download(ctx context.Context, bucket, key, destPath string) error
	Upload(ctx context.Context, bucket, key, srcPath, contentType string) error
}

// GCSProvider implements Provider using Google Cloud Storage.
type GCSProvider struct {
	client *storage.Client
}

// NewGCSProvider initializes a new GCS storage provider.
func NewGCSProvider(ctx context.Context) (*GCSProvider, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}
	return &GCSProvider{client: client}, nil
}

// Close closes the underlying GCS client.
func (g *GCSProvider) Close() error {
	if g.client != nil {
		return g.client.Close()
	}
	return nil
}

// Download downloads an object from GCS to a local file.
func (g *GCSProvider) Download(ctx context.Context, bucket, key, destPath string) error {
	rc, err := g.client.Bucket(bucket).Object(key).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to read object %s from bucket %s: %w", key, bucket, err)
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination dir: %w", err)
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, rc); err != nil {
		return fmt.Errorf("failed to write object data: %w", err)
	}

	return nil
}

// Upload uploads a local file to GCS.
func (g *GCSProvider) Upload(ctx context.Context, bucket, key, srcPath, contentType string) error {
	inFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", srcPath, err)
	}
	defer inFile.Close()

	wc := g.client.Bucket(bucket).Object(key).NewWriter(ctx)
	if contentType != "" {
		wc.ContentType = contentType
	}

	if _, err := io.Copy(wc, inFile); err != nil {
		_ = wc.Close()
		return fmt.Errorf("failed to stream file to GCS: %w", err)
	}

	if err := wc.Close(); err != nil {
		return fmt.Errorf("failed to finalize GCS upload: %w", err)
	}

	return nil
}

// LocalProvider implements Provider for local filesystem testing.
type LocalProvider struct {
	baseDir string
}

// NewLocalProvider initializes a mock storage provider using a local directory.
func NewLocalProvider(baseDir string) *LocalProvider {
	_ = os.MkdirAll(baseDir, 0755)
	return &LocalProvider{baseDir: baseDir}
}

// Download copies from local baseDir/<bucket>/<key> to destPath.
func (l *LocalProvider) Download(ctx context.Context, bucket, key, destPath string) error {
	src := filepath.Join(l.baseDir, bucket, key)
	inFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("local storage object not found: %w", err)
	}
	defer inFile.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, inFile)
	return err
}

// Upload copies srcPath to local baseDir/<bucket>/<key>.
func (l *LocalProvider) Upload(ctx context.Context, bucket, key, srcPath, contentType string) error {
	dest := filepath.Join(l.baseDir, bucket, key)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	inFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer inFile.Close()

	outFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, inFile)
	return err
}
