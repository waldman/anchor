package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/waldman/anchor/config"
)

// Client is the S3 access interface.
type Client interface {
	// GetObject returns nil, nil for a 404 (key not found).
	GetObject(ctx context.Context, key string) ([]byte, error)
	// DownloadTree copies all objects under prefix into destDir, preserving relative paths.
	DownloadTree(ctx context.Context, prefix string, destDir string) error
}

type awsClient struct {
	s3        *awss3.Client
	bucket    string
	keyPrefix string // may be empty; includes trailing slash if set
}

func New(awscfg aws.Config, cfg *config.Config) Client {
	return &awsClient{
		s3:        awss3.NewFromConfig(awscfg),
		bucket:    cfg.S3.BucketName,
		keyPrefix: cfg.S3.KeyPrefix,
	}
}

func (c *awsClient) prefixed(path string) string {
	return c.keyPrefix + path
}

// GetObject returns nil, nil for a 404.
func (c *awsClient) GetObject(ctx context.Context, path string) ([]byte, error) {
	out, err := c.s3.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.prefixed(path)),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, nil
		}
		return nil, fmt.Errorf("s3 get %q: %w", path, err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// DownloadTree lists all objects under prefix and downloads them into destDir.
func (c *awsClient) DownloadTree(ctx context.Context, prefix string, destDir string) error {
	fullPrefix := c.prefixed(prefix)
	paginator := awss3.NewListObjectsV2Paginator(c.s3, &awss3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(fullPrefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list objects under %q: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			rel := strings.TrimPrefix(key, fullPrefix)
			if rel == "" {
				continue
			}
			if err := c.downloadFile(ctx, key, filepath.Join(destDir, filepath.FromSlash(rel))); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *awsClient) downloadFile(ctx context.Context, key, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("mkdir for %q: %w", destPath, err)
	}
	out, err := c.s3.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 get %q: %w", key, err)
	}
	defer out.Body.Close()

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %q: %w", destPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, out.Body); err != nil {
		return fmt.Errorf("write %q: %w", destPath, err)
	}
	return nil
}
