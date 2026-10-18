// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package folder

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	mockrepo "github.com/tmphost/tmphost/backend/internal/repository/mocks"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/storage"
	mockstorage "github.com/tmphost/tmphost/backend/internal/service/storage/mocks"
)

var (
	_now    = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
	_host   = "https://f000000000rs.tmp.host/"
	_prefix = "u7/000000000rs/"
)

type env struct {
	ctrl    Controller
	folders *mockrepo.MockFolderRepository
	users   *mockrepo.MockUserRepository
	storage *mockstorage.MockService
	nextID  int64
	limits  entity.FileSettings
}

func newEnv(t *testing.T) *env {
	t.Helper()
	mc := gomock.NewController(t)
	e := &env{
		folders: mockrepo.NewMockFolderRepository(mc),
		users:   mockrepo.NewMockUserRepository(mc),
		storage: mockstorage.NewMockService(mc),
		limits:  entity.FileSettings{MaxTTLMinutes: 120, MaxUploadMB: 1, Subdomains: true, PublicDomain: "tmp.host"},
	}
	e.ctrl = New(Params{
		Folders: e.folders,
		Users:   e.users,
		Storage: e.storage,
		NewID:   func() int64 { return e.nextID },
		Limits:  func() entity.FileSettings { return e.limits },
		BaseURL: "https://tmphost.io/base/",
		Now:     func() time.Time { return _now },
	})
	return e
}

// liveFolder expects a lookup of folder 1000, owned by user 7, that
// expires in left.
func (e *env) liveFolder(left time.Duration) model.Folder {
	f := model.Folder{ID: 1000, UserID: 7, CreatedAt: _now.Add(-time.Minute), ExpiresAt: _now.Add(left)}
	e.folders.EXPECT().FindFolderByID(gomock.Any(), int64(1000)).Return(f, nil)
	return f
}

func TestNew_DefaultClock(t *testing.T) {
	c := New(Params{}).(*controller)
	assert.WithinDuration(t, time.Now(), c.now(), time.Minute)
	assert.Equal(t, "http", c.scheme, "without a base URL")
	assert.Len(t, idgen.FormatFolderID(c.newID()), idgen.FolderIDLength, "random folder IDs")
}

func TestFolderURLs_PathMode(t *testing.T) {
	e := newEnv(t)
	e.limits.Subdomains = false
	f := e.liveFolder(time.Hour)
	e.storage.EXPECT().List(gomock.Any(), "u7/000000000rs/").Return([]storage.Object{{Key: "u7/000000000rs/site/a b.html", Size: 1, ModTime: _now}}, nil)
	rs, err := e.ctrl.GetFolder(context.Background(), &entity.GetFolderRequest{UserID: f.UserID, FolderID: f.ID})
	require.NoError(t, err)
	assert.Equal(t, "https://tmphost.io/base/files/7/000000000rs/", rs.Folder.URL)
	assert.Equal(t, "https://tmphost.io/base/files/7/000000000rs/site/a%20b.html", rs.Files[0].URL)
}

func TestCost(t *testing.T) {
	assert.Equal(t, 3, Cost(3, 30*time.Minute))
	assert.Equal(t, 6, Cost(3, 31*time.Minute))
	assert.Equal(t, 2, Cost(1, time.Hour))
	assert.Equal(t, 1, Cost(1, time.Second))
	assert.Equal(t, 1, Cost(1, 0), "never free")
}

func TestGetLimits(t *testing.T) {
	e := newEnv(t)
	assert.Equal(t, &entity.GetLimitsResponse{
		DefaultTTLMinutes: 30, MaxTTLMinutes: 120, MaxUploadBytes: 1 << 20, BillingBlockMinutes: 30,
	}, e.ctrl.GetLimits(context.Background()))
}

func TestCreateFolder(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		minutes int
		ttl     time.Duration
	}{"default": {0, DefaultTTL}, "custom": {120, 2 * time.Hour}, "minimum": {1, time.Minute}} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.nextID = 1000
			e.folders.EXPECT().CreateFolder(ctx, model.Folder{ID: 1000, CreatedAt: _now, UserID: 7, ExpiresAt: _now.Add(tc.ttl)}).
				DoAndReturn(func(_ context.Context, f model.Folder) (model.Folder, error) { return f, nil })
			rs, err := e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, TTLMinutes: tc.minutes})
			require.NoError(t, err)
			assert.Equal(t, entity.Folder{ID: 1000, UserID: 7, CreatedAt: _now, ExpiresAt: _now.Add(tc.ttl), URL: _host}, rs.Folder)
		})
	}
}

func TestCreateFolder_Errors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	for _, m := range []int{-1, 121} {
		_, err := e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, TTLMinutes: m})
		assert.ErrorIs(t, err, ErrInvalidTTL)
	}
	e.nextID = 1
	e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).Return(model.Folder{}, errBoom)
	_, err := e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7})
	assert.ErrorIs(t, err, errBoom)
}

func TestListFolders(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.folders.EXPECT().ListFoldersByUser(ctx, int64(7), _now).Return([]model.Folder{{ID: 1000, UserID: 7, ExpiresAt: _now}}, nil)
	rs, err := e.ctrl.ListFolders(ctx, &entity.ListFoldersRequest{UserID: 7})
	require.NoError(t, err)
	require.Len(t, rs.Folders, 1)
	assert.Equal(t, _host, rs.Folders[0].URL)

	e.folders.EXPECT().ListFoldersByUser(ctx, int64(7), _now).Return(nil, errBoom)
	_, err = e.ctrl.ListFolders(ctx, &entity.ListFoldersRequest{UserID: 7})
	assert.ErrorIs(t, err, errBoom)
}

func TestGetFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.liveFolder(time.Hour)
	mod := _now.In(time.FixedZone("x", 3600))
	e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{
		{Key: _prefix + "a b.txt", Size: 3, ModTime: mod},
		{Key: _prefix + "site/index.html", Size: 5, ModTime: mod},
	}, nil)
	rs, err := e.ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	require.NoError(t, err)
	assert.Equal(t, []entity.File{
		{Path: "a b.txt", Size: 3, UpdatedAt: _now, URL: _host + "a%20b.txt"},
		{Path: "site/index.html", Size: 5, UpdatedAt: _now, URL: _host + "site/index.html"},
	}, rs.Files)
	assert.Equal(t, int64(1000), rs.Folder.ID)
}

func TestGetFolder_Errors(t *testing.T) {
	ctx := context.Background()
	rq := &entity.GetFolderRequest{UserID: 7, FolderID: 1000}

	t.Run("missing", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, repository.ErrNotFound)
		_, err := e.ctrl.GetFolder(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("db error", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, errBoom)
		_, err := e.ctrl.GetFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("expired", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(0)
		_, err := e.ctrl.GetFolder(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("someone else's", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		_, err := e.ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 8, FolderID: 1000})
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("storage error", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, errBoom)
		_, err := e.ctrl.GetFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestDeleteFolder(t *testing.T) {
	ctx := context.Background()
	rq := &entity.DeleteFolderRequest{UserID: 7, FolderID: 1000}

	t.Run("ok", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		gomock.InOrder(
			e.storage.EXPECT().DeletePrefix(ctx, _prefix).Return(nil),
			e.folders.EXPECT().DeleteFolder(ctx, int64(1000)).Return(nil),
		)
		require.NoError(t, e.ctrl.DeleteFolder(ctx, rq))
	})
	t.Run("not found", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		assert.ErrorIs(t, e.ctrl.DeleteFolder(ctx, &entity.DeleteFolderRequest{UserID: 8, FolderID: 1000}), ErrNotFound)
	})
	t.Run("storage error keeps the record", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.storage.EXPECT().DeletePrefix(ctx, _prefix).Return(errBoom)
		assert.ErrorIs(t, e.ctrl.DeleteFolder(ctx, rq), errBoom)
	})
	t.Run("db error", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.storage.EXPECT().DeletePrefix(ctx, _prefix).Return(nil)
		e.folders.EXPECT().DeleteFolder(ctx, int64(1000)).Return(errBoom)
		assert.ErrorIs(t, e.ctrl.DeleteFolder(ctx, rq), errBoom)
	})
}

func TestPutFiles(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.liveFolder(45 * time.Minute)
	gomock.InOrder(
		e.users.EXPECT().SpendCredits(ctx, int64(7), 4).Return(nil),
		e.storage.EXPECT().Put(ctx, _prefix+"index.html", []byte("<p>")).Return(nil),
		e.storage.EXPECT().Put(ctx, _prefix+"css/a.css", []byte("a{}")).Return(nil),
	)
	rs, err := e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{
		{Path: "index.html", Data: []byte("<p>")},
		{Path: "css/a.css", Data: []byte("a{}")},
	}})
	require.NoError(t, err)
	assert.Equal(t, 4, rs.CreditsSpent, "2 files x 2 started blocks")
	assert.Equal(t, []entity.File{
		{Path: "index.html", Size: 3, UpdatedAt: _now, URL: _host + "index.html"},
		{Path: "css/a.css", Size: 3, UpdatedAt: _now, URL: _host + "css/a.css"},
	}, rs.Files)
}

func TestPutFiles_Validation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	put := func(files ...entity.FileUpload) error {
		_, err := e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: files})
		return err
	}
	assert.ErrorIs(t, put(), ErrNoFiles)
	assert.ErrorIs(t, put(entity.FileUpload{Path: "../x"}), ErrInvalidPath)
	assert.ErrorIs(t, put(entity.FileUpload{Path: "a"}, entity.FileUpload{Path: "a"}), ErrDuplicatePath)
	assert.ErrorIs(t, put(entity.FileUpload{Path: "a", Data: make([]byte, 1<<20)}, entity.FileUpload{Path: "b", Data: []byte("x")}), ErrTooLarge)
}

func TestPutFiles_Credits(t *testing.T) {
	ctx := context.Background()
	rq := &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "a"}}}

	t.Run("not found", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		_, err := e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 8, FolderID: 1000, Files: rq.Files})
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("insufficient", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.users.EXPECT().SpendCredits(ctx, int64(7), 2).Return(repository.ErrInsufficientCredits)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, ErrInsufficientCredits)
		assert.Contains(t, err.Error(), "2 needed")
	})
	t.Run("spend error", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.users.EXPECT().SpendCredits(ctx, int64(7), 2).Return(errBoom)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestPutFiles_WriteFailureRefundsTheRest(t *testing.T) {
	ctx := context.Background()
	rq := &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "a"}, {Path: "b"}, {Path: "c"}}}

	t.Run("refunded", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(30 * time.Minute)
		gomock.InOrder(
			e.users.EXPECT().SpendCredits(ctx, int64(7), 3).Return(nil),
			e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(nil),
			e.storage.EXPECT().Put(ctx, _prefix+"b", gomock.Any()).Return(errBoom),
			e.users.EXPECT().RefundCredits(ctx, int64(7), 2).Return(nil),
		)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.Contains(t, err.Error(), "write b")
	})
	t.Run("refund fails too", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(30 * time.Minute)
		refundErr := errors.New("refund failed")
		e.users.EXPECT().SpendCredits(ctx, int64(7), 3).Return(nil)
		e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(errBoom)
		e.users.EXPECT().RefundCredits(ctx, int64(7), 3).Return(refundErr)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.ErrorIs(t, err, refundErr)
	})
}

func TestDeleteFile(t *testing.T) {
	ctx := context.Background()
	rq := &entity.DeleteFileRequest{UserID: 7, FolderID: 1000, Path: "site/a.txt"}

	e := newEnv(t)
	assert.ErrorIs(t, e.ctrl.DeleteFile(ctx, &entity.DeleteFileRequest{UserID: 7, FolderID: 1000, Path: "/a"}), ErrInvalidPath)

	e.liveFolder(time.Hour)
	e.storage.EXPECT().Delete(ctx, _prefix+"site/a.txt").Return(nil)
	require.NoError(t, e.ctrl.DeleteFile(ctx, rq))

	e.liveFolder(time.Hour)
	e.storage.EXPECT().Delete(ctx, _prefix+"site/a.txt").Return(errBoom)
	assert.ErrorIs(t, e.ctrl.DeleteFile(ctx, rq), errBoom)

	e.liveFolder(0)
	assert.ErrorIs(t, e.ctrl.DeleteFile(ctx, rq), ErrNotFound)
}

func TestOpenPublicFile(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct{ path, key, contentType string }{
		"file":         {"site/app.js", "site/app.js", "text/javascript; charset=utf-8"},
		"root index":   {"", "index.html", "text/html; charset=utf-8"},
		"dir index":    {"site/", "site/index.html", "text/html; charset=utf-8"},
		"unknown ext":  {"blob.zzz", "blob.zzz", "application/octet-stream"},
		"no extension": {"LICENSE", "LICENSE", "application/octet-stream"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.liveFolder(time.Hour)
			body := io.NopCloser(strings.NewReader("x"))
			e.storage.EXPECT().Get(ctx, _prefix+tc.key).Return(body, storage.Object{Size: 1, ModTime: _now}, nil)
			rs, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: tc.path})
			require.NoError(t, err)
			assert.Equal(t, &entity.OpenFileResponse{Body: body, Size: 1, ContentType: tc.contentType, ModTime: _now}, rs)
		})
	}
	t.Run("folder host, which names no owner", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		body := io.NopCloser(strings.NewReader("x"))
		e.storage.EXPECT().Get(ctx, _prefix+"index.html").Return(body, storage.Object{Size: 1, ModTime: _now}, nil)
		rs, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000})
		require.NoError(t, err)
		assert.Equal(t, body, rs.Body)
	})
}

func TestOpenPublicFile_Errors(t *testing.T) {
	ctx := context.Background()
	rq := &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: "a.txt"}

	e := newEnv(t)
	_, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: "../a"})
	assert.ErrorIs(t, err, ErrNotFound)

	e.liveFolder(time.Hour)
	_, err = e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{UserID: 8, FolderID: 1000, Path: "a.txt"})
	assert.ErrorIs(t, err, ErrNotFound, "the path names another owner")

	e.liveFolder(0)
	_, err = e.ctrl.OpenPublicFile(ctx, rq)
	assert.ErrorIs(t, err, ErrNotFound, "expired")

	e.liveFolder(time.Hour)
	e.storage.EXPECT().Get(ctx, _prefix+"a.txt").Return(nil, storage.Object{}, storage.ErrNotFound)
	_, err = e.ctrl.OpenPublicFile(ctx, rq)
	assert.ErrorIs(t, err, ErrNotFound)

	e.liveFolder(time.Hour)
	e.storage.EXPECT().Get(ctx, _prefix+"a.txt").Return(nil, storage.Object{}, errBoom)
	_, err = e.ctrl.OpenPublicFile(ctx, rq)
	assert.ErrorIs(t, err, errBoom)
}

func TestOpenFile(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	_, err := e.ctrl.OpenFile(ctx, &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "dir/"})
	assert.ErrorIs(t, err, ErrInvalidPath, "no index fallback for downloads")

	e.liveFolder(time.Hour)
	_, err = e.ctrl.OpenFile(ctx, &entity.OpenFileRequest{UserID: 8, FolderID: 1000, Path: "a.txt"})
	assert.ErrorIs(t, err, ErrNotFound)

	e.liveFolder(time.Hour)
	body := io.NopCloser(strings.NewReader("x"))
	e.storage.EXPECT().Get(ctx, _prefix+"dir/a.txt").Return(body, storage.Object{Size: 1, ModTime: _now}, nil)
	rs, err := e.ctrl.OpenFile(ctx, &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "dir/a.txt"})
	require.NoError(t, err)
	assert.Equal(t, &entity.OpenFileResponse{Body: body, Size: 1, ContentType: "text/plain; charset=utf-8", ModTime: _now}, rs)
}

func TestFolderHost(t *testing.T) {
	assert.Equal(t, "f000000000rs", FolderHost(1000))
}

func TestDeleteExpiredFolders(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.folders.EXPECT().ListExpiredFolders(ctx, _now, _sweepBatch).Return([]model.Folder{
		{ID: 1, UserID: 7}, {ID: 2, UserID: 8}, {ID: 3, UserID: 9},
	}, nil)
	e.storage.EXPECT().DeletePrefix(ctx, "u7/00000000001/").Return(nil)
	e.folders.EXPECT().DeleteFolder(ctx, int64(1)).Return(nil)
	e.storage.EXPECT().DeletePrefix(ctx, "u8/00000000002/").Return(errBoom)
	e.storage.EXPECT().DeletePrefix(ctx, "u9/00000000003/").Return(nil)
	e.folders.EXPECT().DeleteFolder(ctx, int64(3)).Return(nil)

	rs, err := e.ctrl.DeleteExpiredFolders(ctx)
	assert.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "folder 2")
	assert.Equal(t, 2, rs.Deleted, "a failure does not stop the batch")

	e.folders.EXPECT().ListExpiredFolders(ctx, _now, _sweepBatch).Return(nil, errBoom)
	rs, err = e.ctrl.DeleteExpiredFolders(ctx)
	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, 0, rs.Deleted)

	e.folders.EXPECT().ListExpiredFolders(ctx, _now, _sweepBatch).Return(nil, nil)
	rs, err = e.ctrl.DeleteExpiredFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, rs.Deleted)
}

func TestValidPath(t *testing.T) {
	for _, p := range []string{"a", "index.html", "site/css/a b.css", ".well-known/x", "ünïcode/文件.txt", strings.Repeat("a", 255), "1/2/3/4/5/f"} {
		assert.True(t, ValidPath(p), p)
	}
	for _, p := range []string{
		"", "/a", "a/", "a//b", "./a", "a/../b", "..", "a\\b", "a\x00b", "a\nb", "a\x7fb",
		"\xff", strings.Repeat("a", 256), strings.Repeat("a/", 256) + "a", ".upload-1", "x/.upload-tmp", "1/2/3/4/5/6/f",
	} {
		assert.False(t, ValidPath(p), p)
	}
}
