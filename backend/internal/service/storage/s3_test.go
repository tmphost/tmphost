// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/service/storage"
	"github.com/tmphost/tmphost/backend/internal/service/storage/mocks"
)

var errBoom = errors.New("boom")

func newS3(t *testing.T) (storage.Service, *mocks.MockS3API) {
	t.Helper()
	client := mocks.NewMockS3API(gomock.NewController(t))
	return storage.NewS3WithClient(client, nil, "bucket"), client
}

func TestNewS3(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "none"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "none"))
	s, err := storage.NewS3(context.Background(), storage.S3Config{Region: "auto", Bucket: "b", Endpoint: "http://127.0.0.1:1", AccessKeyID: "a", SecretAccessKey: "s", UsePathStyle: true})
	require.NoError(t, err)
	assert.NotNil(t, s)

	s, err = storage.NewS3(context.Background(), storage.S3Config{Region: "us-east-1", Bucket: "b"})
	require.NoError(t, err)
	assert.NotNil(t, s)
}

func TestNewS3_ConfigError(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(cfg, []byte("[default]\n"), 0o600))
	t.Setenv("AWS_CONFIG_FILE", cfg)
	t.Setenv("AWS_PROFILE", "missing")
	_, err := storage.NewS3(context.Background(), storage.S3Config{Region: "r", Bucket: "b"})
	assert.Error(t, err)
}

func TestS3_Put(t *testing.T) {
	s, client := newS3(t)
	client.EXPECT().PutObject(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			assert.Equal(t, "bucket", aws.ToString(in.Bucket))
			assert.Equal(t, "u1/f/a", aws.ToString(in.Key))
			assert.Equal(t, int64(2), aws.ToInt64(in.ContentLength))
			return &s3.PutObjectOutput{}, nil
		})
	require.NoError(t, s.Put(context.Background(), "u1/f/a", []byte("hi")))
	assert.ErrorIs(t, s.Put(context.Background(), "../a", nil), storage.ErrInvalidKey)
}

func TestS3_Get(t *testing.T) {
	ctx := context.Background()
	s, client := newS3(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	client.EXPECT().GetObject(gomock.Any(), gomock.Any()).Return(&s3.GetObjectOutput{
		Body: io.NopCloser(strings.NewReader("hi")), ContentLength: aws.Int64(2), LastModified: &at,
	}, nil)
	rc, obj, err := s.Get(ctx, "u1/f/a")
	require.NoError(t, err)
	b, _ := io.ReadAll(rc)
	assert.Equal(t, "hi", string(b))
	assert.Equal(t, storage.Object{Key: "u1/f/a", Size: 2, ModTime: at}, obj)

	client.EXPECT().GetObject(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("wrapped: %w", &types.NoSuchKey{}))
	_, _, err = s.Get(ctx, "u1/f/missing")
	assert.ErrorIs(t, err, storage.ErrNotFound)

	client.EXPECT().GetObject(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	_, _, err = s.Get(ctx, "u1/f/a")
	assert.ErrorIs(t, err, errBoom)

	_, _, err = s.Get(ctx, "")
	assert.ErrorIs(t, err, storage.ErrInvalidKey)
}

func TestS3_SignGet(t *testing.T) {
	ctx := context.Background()
	// A real presigner: signing is local, so it needs no bucket.
	presigner := s3.NewPresignClient(s3.New(s3.Options{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("AKID", "secret", ""),
	}))
	s := storage.NewS3WithClient(nil, presigner, "bucket").(storage.Signer)

	u, err := s.SignGet(ctx, "u1/f/a b.bin", storage.SignOptions{
		TTL: 15 * time.Minute, ContentType: "application/octet-stream", ContentDisposition: `attachment; filename="a b.bin"`,
	})
	require.NoError(t, err)
	parsed, err := url.Parse(u)
	require.NoError(t, err)
	assert.Equal(t, "bucket.s3.us-east-1.amazonaws.com", parsed.Host)
	assert.Equal(t, "/u1/f/a%20b.bin", parsed.EscapedPath())
	q := parsed.Query()
	assert.Equal(t, "900", q.Get("X-Amz-Expires"))
	assert.Equal(t, "application/octet-stream", q.Get("response-content-type"))
	assert.Equal(t, `attachment; filename="a b.bin"`, q.Get("response-content-disposition"))
	assert.NotEmpty(t, q.Get("X-Amz-Signature"))

	u, err = s.SignGet(ctx, "u1/f/a", storage.SignOptions{TTL: time.Minute})
	require.NoError(t, err)
	parsed, _ = url.Parse(u)
	assert.Empty(t, parsed.Query().Get("response-content-type"), "no overrides asked for")
	assert.Empty(t, parsed.Query().Get("response-content-disposition"))

	_, err = s.SignGet(ctx, "../a", storage.SignOptions{})
	assert.ErrorIs(t, err, storage.ErrInvalidKey)

	failing := mocks.NewMockS3Presigner(gomock.NewController(t))
	failing.EXPECT().PresignGetObject(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errBoom)
	_, err = storage.NewS3WithClient(nil, failing, "bucket").(storage.Signer).SignGet(ctx, "u1/f/a", storage.SignOptions{TTL: time.Minute})
	assert.ErrorIs(t, err, errBoom)
}

func TestS3_ListPaginates(t *testing.T) {
	s, client := newS3(t)
	gomock.InOrder(
		client.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
				assert.Equal(t, "u1/f/", aws.ToString(in.Prefix))
				return &s3.ListObjectsV2Output{
					Contents:              []types.Object{{Key: aws.String("u1/f/a"), Size: aws.Int64(1)}},
					IsTruncated:           aws.Bool(true),
					NextContinuationToken: aws.String("next"),
				}, nil
			}),
		client.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
			Contents: []types.Object{{Key: aws.String("u1/f/b"), Size: aws.Int64(2)}},
		}, nil),
	)
	objs, err := s.List(context.Background(), "u1/f/")
	require.NoError(t, err)
	assert.Equal(t, []storage.Object{{Key: "u1/f/a", Size: 1}, {Key: "u1/f/b", Size: 2}}, objs)
}

func TestS3_ListErrors(t *testing.T) {
	s, client := newS3(t)
	client.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errBoom)
	_, err := s.List(context.Background(), "u1/f/")
	assert.ErrorIs(t, err, errBoom)
	_, err = s.List(context.Background(), "u1/f")
	assert.ErrorIs(t, err, storage.ErrInvalidKey)
}

func TestS3_Delete(t *testing.T) {
	s, client := newS3(t)
	client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(&s3.DeleteObjectOutput{}, nil)
	require.NoError(t, s.Delete(context.Background(), "u1/f/a"))
	assert.ErrorIs(t, s.Delete(context.Background(), "a/./b"), storage.ErrInvalidKey)
}

func TestS3_DeletePrefixBatches(t *testing.T) {
	s, client := newS3(t)
	contents := make([]types.Object, storage.S3DeleteBatch+1)
	for i := range contents {
		contents[i] = types.Object{Key: aws.String(fmt.Sprintf("u1/f/%d", i))}
	}
	client.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{Contents: contents}, nil)
	var sizes []int
	client.EXPECT().DeleteObjects(gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, in *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
			sizes = append(sizes, len(in.Delete.Objects))
			return &s3.DeleteObjectsOutput{}, nil
		})
	require.NoError(t, s.DeletePrefix(context.Background(), "u1/f/"))
	assert.Equal(t, []int{storage.S3DeleteBatch, 1}, sizes)
}

func TestS3_DeletePrefixErrors(t *testing.T) {
	ctx := context.Background()
	s, client := newS3(t)
	list := &s3.ListObjectsV2Output{Contents: []types.Object{{Key: aws.String("u1/f/a")}}}

	client.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errBoom)
	assert.ErrorIs(t, s.DeletePrefix(ctx, "u1/f/"), errBoom)

	client.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(list, nil)
	client.EXPECT().DeleteObjects(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	assert.ErrorIs(t, s.DeletePrefix(ctx, "u1/f/"), errBoom)

	client.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(list, nil)
	client.EXPECT().DeleteObjects(gomock.Any(), gomock.Any()).Return(&s3.DeleteObjectsOutput{
		Errors: []types.Error{{Key: aws.String("u1/f/a"), Message: aws.String("denied")}},
	}, nil)
	assert.EqualError(t, s.DeletePrefix(ctx, "u1/f/"), "s3: delete u1/f/a: denied")
}
