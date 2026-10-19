// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package folder

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/markdown"
	"github.com/tmphost/tmphost/backend/internal/repository"
	mockrepo "github.com/tmphost/tmphost/backend/internal/repository/mocks"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/quota"
	"github.com/tmphost/tmphost/backend/internal/service/storage"
	mockstorage "github.com/tmphost/tmphost/backend/internal/service/storage/mocks"
)

var (
	_now    = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
	_host   = "https://000000rs.tmp.host/"
	_prefix = "u7/000000rs/"
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
		limits:  entity.FileSettings{MaxTTLMinutes: 120, MaxUploadMB: 1, AnonymousMaxTTLMinutes: 10, Subdomains: true, PublicDomain: "tmp.host"},
	}
	e.ctrl = New(Params{
		Folders:        e.folders,
		Users:          e.users,
		Storage:        e.storage,
		Anonymous:      quota.New(quota.Params{Limit: AnonymousOps, Window: AnonymousWindow, Now: func() time.Time { return _now }}),
		AnonymousBytes: quota.New(quota.Params{Limit: AnonymousBytes, Window: AnonymousWindow, Now: func() time.Time { return _now }}),
		NewID:          func() int64 { return e.nextID },
		Limits:         func() entity.FileSettings { return e.limits },
		BaseURL:        "https://tmphost.io/base/",
		Now:            func() time.Time { return _now },
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
	e.storage.EXPECT().List(gomock.Any(), "u7/000000rs/").Return([]storage.Object{{Key: "u7/000000rs/site/a b.html", Size: 1, ModTime: _now}}, nil)
	e.folders.EXPECT().ListFiles(gomock.Any(), int64(1000)).Return(nil, nil)
	rs, err := e.ctrl.GetFolder(context.Background(), &entity.GetFolderRequest{UserID: f.UserID, FolderID: f.ID})
	require.NoError(t, err)
	assert.Equal(t, "https://tmphost.io/base/files/7/000000rs/", rs.Folder.URL)
	assert.Equal(t, "https://tmphost.io/base/files/7/000000rs/site/a%20b.html", rs.Files[0].URL)
}

func TestCost(t *testing.T) {
	const mb = 1 << 20
	assert.Equal(t, 1, Cost(mb, 30*time.Minute))
	assert.Equal(t, 2, Cost(mb+mb/100, 30*time.Minute), "1.01 MB")
	assert.Equal(t, 3, Cost(2*mb+mb/100, 30*time.Minute), "2.01 MB")
	assert.Equal(t, 6, Cost(2*mb+1, 31*time.Minute), "3 units x 2 started blocks")
	assert.Equal(t, 2, Cost(1, time.Hour))
	assert.Equal(t, 1, Cost(1, time.Second))
	assert.Equal(t, 1, Cost(0, 0), "never free")
}

func TestGetLimits(t *testing.T) {
	e := newEnv(t)
	assert.Equal(t, &entity.GetLimitsResponse{
		DefaultTTLMinutes: 30, MaxTTLMinutes: 120, MaxUploadBytes: 1 << 20, BillingBlockMinutes: 30, BillingUnitBytes: 1 << 20,
	}, e.ctrl.GetLimits(context.Background(), &entity.GetLimitsRequest{}))
	assert.Equal(t, &entity.GetLimitsResponse{
		DefaultTTLMinutes: 10, MaxTTLMinutes: 10, MaxUploadBytes: 1 << 20, BillingBlockMinutes: 30, BillingUnitBytes: 1 << 20,
		Anonymous: true, FileOps: 5, FileBytes: 4 << 20, FileOpsWindowMinutes: 30,
	}, e.ctrl.GetLimits(context.Background(), &entity.GetLimitsRequest{Anonymous: true}))
}

func TestCreateFolder_Anonymous(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.nextID = 1000
	_, err := e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, Anonymous: true, TTLMinutes: 11})
	assert.ErrorIs(t, err, ErrInvalidTTL, "anonymous folders live at most 10 minutes")

	e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).Return(model.Folder{}, errBoom)
	_, err = e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, Anonymous: true})
	assert.ErrorIs(t, err, errBoom)

	e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).
		DoAndReturn(func(_ context.Context, f model.Folder) (model.Folder, error) {
			assert.Equal(t, 10*time.Minute, f.ExpiresAt.Sub(f.CreatedAt), "no TTL asked: the anonymous max, not DefaultTTL")
			return f, nil
		}).Times(AnonymousOps)
	for range AnonymousOps {
		_, err = e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, Anonymous: true})
		require.NoError(t, err, "a failed create gives its folder back")
	}
	_, err = e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, Anonymous: true})
	assert.ErrorIs(t, err, ErrQuotaExceeded)

	e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).
		DoAndReturn(func(_ context.Context, f model.Folder) (model.Folder, error) { return f, nil })
	_, err = e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 8, Anonymous: true})
	require.NoError(t, err, "each user has their own quota")
}

func TestPutFiles_Anonymous(t *testing.T) {
	ctx := context.Background()
	put := func(e *env, files ...entity.FileUpload) (*entity.PutFilesResponse, error) {
		return e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, Anonymous: true, FolderID: 1000, Files: files})
	}

	t.Run("quota, not credits", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().SaveFiles(ctx, int64(1000), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(nil)
		e.storage.EXPECT().Put(ctx, _prefix+"b", gomock.Any()).Return(nil)
		rs, err := put(e, entity.FileUpload{Path: "a", Data: make([]byte, 1<<20)}, entity.FileUpload{Path: "b", Data: make([]byte, 1<<20)})
		require.NoError(t, err, "2 MB, over the signed-in request limit of 1 MB")
		assert.Zero(t, rs.CreditsSpent)
		assert.Len(t, rs.Files, 2)

		e.liveFolder(30 * time.Minute)
		_, err = put(e, entity.FileUpload{Path: "c"}, entity.FileUpload{Path: "d"}, entity.FileUpload{Path: "e"}, entity.FileUpload{Path: "f"})
		assert.ErrorIs(t, err, ErrQuotaExceeded, "2 + 4 is over 5")

		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(3)
		_, err = put(e, entity.FileUpload{Path: "c"}, entity.FileUpload{Path: "d"}, entity.FileUpload{Path: "e"})
		require.NoError(t, err)

		e.liveFolder(30 * time.Minute)
		_, err = put(e, entity.FileUpload{Path: "g"})
		assert.ErrorIs(t, err, ErrQuotaExceeded)
	})
	t.Run("too large", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().SaveFiles(ctx, int64(1000), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		_, err := put(e, entity.FileUpload{Path: "a"}, entity.FileUpload{Path: "b", Data: make([]byte, 1<<20+1)})
		assert.ErrorIs(t, err, ErrTooLarge)
		assert.ErrorContains(t, err, `"b" is over the 1 MB file limit`)
	})
	t.Run("bytes quota", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().SaveFiles(ctx, int64(1000), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(3)
		mb := func(p string) entity.FileUpload { return entity.FileUpload{Path: p, Data: make([]byte, 1<<20)} }
		_, err := put(e, mb("a"), mb("b"), mb("c"))
		require.NoError(t, err)

		e.liveFolder(30 * time.Minute)
		_, err = put(e, mb("d"), entity.FileUpload{Path: "e", Data: []byte("x")})
		require.ErrorIs(t, err, ErrQuotaExceeded, "3 MB + 1 MB and a byte is over 4 MB")
		assert.ErrorContains(t, err, "at most 4 MB per 30 minutes")

		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(2)
		_, err = put(e, mb("d"), entity.FileUpload{Path: "e"})
		require.NoError(t, err, "the refused files were given back: 3 + 2 files, 4 MB")
	})
	t.Run("write failure gives the rest back", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().SaveFiles(ctx, int64(1000), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mb := func(p string) entity.FileUpload { return entity.FileUpload{Path: p, Data: make([]byte, 1<<20)} }
		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(nil)
		e.storage.EXPECT().Put(ctx, _prefix+"b", gomock.Any()).Return(errBoom)
		_, err := put(e, mb("a"), mb("b"), mb("c"))
		assert.ErrorIs(t, err, errBoom)
		assert.NotContains(t, err.Error(), "refund")

		e.liveFolder(30 * time.Minute)
		e.storage.EXPECT().Put(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(4)
		_, err = put(e, mb("b"), mb("c"), mb("d"), entity.FileUpload{Path: "e"})
		require.NoError(t, err, "only the written file counts: 1 + 4 files, 1 + 3 MB")
	})
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
			e.folders.EXPECT().CreateFolder(ctx, model.Folder{ID: 1000, CreatedAt: _now, UserID: 7, ExpiresAt: _now.Add(tc.ttl), Agent: "Claude Code", Files: model.FolderFiles{}}).
				DoAndReturn(func(_ context.Context, f model.Folder) (model.Folder, error) { return f, nil })
			rs, err := e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7, Agent: "Claude Code", TTLMinutes: tc.minutes})
			require.NoError(t, err)
			assert.Equal(t, entity.Folder{ID: 1000, UserID: 7, CreatedAt: _now, ExpiresAt: _now.Add(tc.ttl), URL: _host, DefaultTitle: "000000rs", Agent: "Claude Code"}, rs.Folder)
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
	e.folders.EXPECT().ListFoldersByUser(ctx, int64(7), _now).Return([]model.Folder{
		{ID: 1000, UserID: 7, ExpiresAt: _now, Title: "Photos"},
		{ID: 1001, UserID: 7, ExpiresAt: _now},
		{ID: 1002, UserID: 7, ExpiresAt: _now},
		{ID: 1003, UserID: 7, ExpiresAt: _now},
	}, nil)
	e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{{Key: _prefix + "a"}, {Key: _prefix + "b"}}, nil)
	e.storage.EXPECT().List(ctx, "u7/000000rt/").Return([]storage.Object{{Key: "u7/000000rt/site/report.pdf"}}, nil)
	e.storage.EXPECT().List(ctx, "u7/000000ru/").Return(nil, nil)
	e.storage.EXPECT().List(ctx, "u7/000000rv/").Return(nil, errBoom)
	rs, err := e.ctrl.ListFolders(ctx, &entity.ListFoldersRequest{UserID: 7})
	require.NoError(t, err)
	require.Len(t, rs.Folders, 4)
	assert.Equal(t, _host, rs.Folders[0].URL)
	assert.Equal(t, "Photos", rs.Folders[0].Title)
	titles := []string{}
	for _, f := range rs.Folders {
		titles = append(titles, f.DefaultTitle)
	}
	assert.Equal(t, []string{"000000rs", "report.pdf", "000000ru", "000000rv"}, titles, "only file's name, else the ID")

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
	// A file without a record (e.g. its record failed to save) has no agent.
	e.folders.EXPECT().ListFiles(ctx, int64(1000)).Return([]model.File{{FolderID: 1000, Path: "site/index.html", Agent: "Claude Code"}}, nil)
	rs, err := e.ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	require.NoError(t, err)
	assert.Equal(t, []entity.File{
		{Path: "a b.txt", Size: 3, UpdatedAt: _now, URL: _host + "a%20b.txt"},
		{Path: "site/index.html", Size: 5, UpdatedAt: _now, URL: _host + "site/index.html", Agent: "Claude Code"},
	}, rs.Files)
	assert.Equal(t, int64(1000), rs.Folder.ID)
	assert.Equal(t, "000000rs", rs.Folder.DefaultTitle)

	e.liveFolder(time.Hour)
	e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
	e.folders.EXPECT().ListFiles(ctx, int64(1000)).Return(nil, errBoom)
	_, err = e.ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	assert.ErrorIs(t, err, errBoom)
}

func TestGetFolder_SingleFileDefaultTitle(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.liveFolder(time.Hour)
	e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{{Key: _prefix + "notes.txt"}}, nil)
	e.folders.EXPECT().ListFiles(ctx, int64(1000)).Return(nil, nil)
	rs, err := e.ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	require.NoError(t, err)
	assert.Equal(t, "notes.txt", rs.Folder.DefaultTitle)
}

func TestUpdateFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.liveFolder(time.Hour)
	e.folders.EXPECT().UpdateFolderTitle(ctx, int64(1000), "Holiday 🌴").Return(nil)
	e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{{Key: _prefix + "beach.jpg"}}, nil)
	rs, err := e.ctrl.UpdateFolder(ctx, &entity.UpdateFolderRequest{UserID: 7, FolderID: 1000, Title: "  Holiday 🌴 \t"})
	require.NoError(t, err)
	assert.Equal(t, "Holiday 🌴", rs.Folder.Title)
	assert.Equal(t, "beach.jpg", rs.Folder.DefaultTitle)
	assert.Equal(t, _host, rs.Folder.URL)

	e.liveFolder(time.Hour)
	e.folders.EXPECT().UpdateFolderTitle(ctx, int64(1000), "").Return(nil)
	e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
	rs, err = e.ctrl.UpdateFolder(ctx, &entity.UpdateFolderRequest{UserID: 7, FolderID: 1000, Title: "   "})
	require.NoError(t, err)
	assert.Empty(t, rs.Folder.Title, "blank clears the title")
	assert.Equal(t, "000000rs", rs.Folder.DefaultTitle)
}

func TestUpdateFolder_Errors(t *testing.T) {
	ctx := context.Background()
	rq := func(title string) *entity.UpdateFolderRequest {
		return &entity.UpdateFolderRequest{UserID: 7, FolderID: 1000, Title: title}
	}

	t.Run("invalid", func(t *testing.T) {
		e := newEnv(t)
		for _, title := range []string{strings.Repeat("é", MaxTitleLength+1), "a\x00b", "a\nb", "\xff"} {
			_, err := e.ctrl.UpdateFolder(ctx, rq(title))
			assert.ErrorIs(t, err, ErrInvalidTitle, title)
		}
	})
	t.Run("not owner", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		_, err := e.ctrl.UpdateFolder(ctx, &entity.UpdateFolderRequest{UserID: 8, FolderID: 1000, Title: "x"})
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("update fails", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.folders.EXPECT().UpdateFolderTitle(ctx, int64(1000), "x").Return(errBoom)
		_, err := e.ctrl.UpdateFolder(ctx, rq("x"))
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("list fails", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.folders.EXPECT().UpdateFolderTitle(ctx, int64(1000), "x").Return(nil)
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, errBoom)
		_, err := e.ctrl.UpdateFolder(ctx, rq("x"))
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestExtendFolder(t *testing.T) {
	ctx := context.Background()
	rq := &entity.ExtendFolderRequest{UserID: 7, Agent: "Claude Code", FolderID: 1000, Minutes: 45}

	e := newEnv(t)
	e.liveFolder(time.Hour)
	e.storage.EXPECT().List(ctx, _prefix).Return([]storage.Object{
		{Key: _prefix + "a.jpg", Size: 3 << 19}, {Key: _prefix + "b.txt", Size: 10 << 10},
	}, nil)
	// (2 units + 1 unit) x 2 started blocks of 45 minutes.
	gomock.InOrder(
		e.users.EXPECT().SpendCredits(ctx, int64(7), 6, "Claude Code").Return(nil),
		e.folders.EXPECT().ExtendFolder(ctx, int64(1000), _now, _now.Add(105*time.Minute)).Return(nil),
	)
	rs, err := e.ctrl.ExtendFolder(ctx, rq)
	require.NoError(t, err)
	assert.Equal(t, 6, rs.CreditsSpent)
	assert.Equal(t, _now.Add(105*time.Minute), rs.Folder.ExpiresAt)
	assert.Equal(t, "000000rs", rs.Folder.DefaultTitle)
	assert.Equal(t, _host, rs.Folder.URL)

	e = newEnv(t)
	e.liveFolder(time.Hour)
	e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
	e.folders.EXPECT().ExtendFolder(ctx, int64(1000), _now, _now.Add(2*time.Hour)).Return(nil)
	rs, err = e.ctrl.ExtendFolder(ctx, &entity.ExtendFolderRequest{UserID: 7, FolderID: 1000, Minutes: 60})
	require.NoError(t, err)
	assert.Zero(t, rs.CreditsSpent, "an empty folder is free, up to the max TTL")
}

func TestExtendFolder_Errors(t *testing.T) {
	ctx := context.Background()
	rq := &entity.ExtendFolderRequest{UserID: 7, FolderID: 1000, Minutes: 30}
	file := []storage.Object{{Key: _prefix + "a", Size: 1}}

	t.Run("not owner", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		_, err := e.ctrl.ExtendFolder(ctx, &entity.ExtendFolderRequest{UserID: 8, FolderID: 1000, Minutes: 30})
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("invalid minutes", func(t *testing.T) {
		for _, minutes := range []int{0, -5, 61} {
			e := newEnv(t)
			e.liveFolder(time.Hour)
			_, err := e.ctrl.ExtendFolder(ctx, &entity.ExtendFolderRequest{UserID: 7, FolderID: 1000, Minutes: minutes})
			assert.ErrorIs(t, err, ErrInvalidTTL, minutes)
			assert.Contains(t, err.Error(), "at most 120 more minutes")
		}
	})
	t.Run("list fails", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, errBoom)
		_, err := e.ctrl.ExtendFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("insufficient", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.storage.EXPECT().List(ctx, _prefix).Return(file, nil)
		e.users.EXPECT().SpendCredits(ctx, int64(7), 1, "").Return(repository.ErrInsufficientCredits)
		_, err := e.ctrl.ExtendFolder(ctx, rq)
		assert.ErrorIs(t, err, ErrInsufficientCredits)
		assert.Contains(t, err.Error(), "1 needed")
	})
	t.Run("spend error", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.storage.EXPECT().List(ctx, _prefix).Return(file, nil)
		e.users.EXPECT().SpendCredits(ctx, int64(7), 1, "").Return(errBoom)
		_, err := e.ctrl.ExtendFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("expired meanwhile refunds", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.storage.EXPECT().List(ctx, _prefix).Return(file, nil)
		e.users.EXPECT().SpendCredits(ctx, int64(7), 1, "").Return(nil)
		e.folders.EXPECT().ExtendFolder(ctx, int64(1000), _now, _now.Add(90*time.Minute)).Return(repository.ErrNotFound)
		e.users.EXPECT().RefundCredits(ctx, int64(7), 1, "").Return(nil)
		_, err := e.ctrl.ExtendFolder(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("update and refund fail", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		refundErr := errors.New("refund failed")
		e.storage.EXPECT().List(ctx, _prefix).Return(file, nil)
		e.users.EXPECT().SpendCredits(ctx, int64(7), 1, "").Return(nil)
		e.folders.EXPECT().ExtendFolder(ctx, int64(1000), _now, _now.Add(90*time.Minute)).Return(errBoom)
		e.users.EXPECT().RefundCredits(ctx, int64(7), 1, "").Return(refundErr)
		_, err := e.ctrl.ExtendFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.ErrorIs(t, err, refundErr)
		assert.NotErrorIs(t, err, ErrNotFound)
	})
	t.Run("empty folder update fails", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, nil)
		e.folders.EXPECT().ExtendFolder(ctx, int64(1000), _now, _now.Add(90*time.Minute)).Return(errBoom)
		_, err := e.ctrl.ExtendFolder(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestValidTitle(t *testing.T) {
	assert.True(t, ValidTitle(""))
	assert.True(t, ValidTitle(strings.Repeat("ü", MaxTitleLength)))
	assert.False(t, ValidTitle(strings.Repeat("ü", MaxTitleLength+1)))
	assert.False(t, ValidTitle("tab\there"))
	assert.False(t, ValidTitle("\u0085"))
	assert.False(t, ValidTitle("\xc3"))
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
	e.limits.MaxUploadMB = 4
	e.liveFolder(45 * time.Minute)
	big := make([]byte, 2<<20+1)
	gomock.InOrder(
		e.users.EXPECT().SpendCredits(ctx, int64(7), 8, "tmphost CLI").Return(nil),
		e.storage.EXPECT().Put(ctx, _prefix+"index.html", []byte("<p>")).Return(nil),
		e.storage.EXPECT().Put(ctx, _prefix+"css/a.bin", big).Return(nil),
		e.folders.EXPECT().SaveFiles(ctx, int64(1000), "tmphost CLI", model.FolderFiles{
			"index.html": {Size: 3, ContentType: "text/html; charset=utf-8", UpdatedAt: _now},
			"css/a.bin":  {Size: 2<<20 + 1, ContentType: "application/octet-stream", UpdatedAt: _now},
		}).Return(nil),
	)
	rs, err := e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, Agent: "tmphost CLI", FolderID: 1000, Files: []entity.FileUpload{
		{Path: "index.html", Data: []byte("<p>")},
		{Path: "css/a.bin", Data: big},
	}})
	require.NoError(t, err)
	assert.Equal(t, 8, rs.CreditsSpent, "(1 + 3 MB units) x 2 started blocks")
	assert.Equal(t, []entity.File{
		{Path: "index.html", Size: 3, UpdatedAt: _now, URL: _host + "index.html", Agent: "tmphost CLI"},
		{Path: "css/a.bin", Size: 2<<20 + 1, UpdatedAt: _now, URL: _host + "css/a.bin", Agent: "tmphost CLI"},
	}, rs.Files)
}

// The files are stored and paid for before their records are saved, so a
// failing save still answers with the files.
func TestPutFiles_RecordFails(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	old := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = old })

	e := newEnv(t)
	e.liveFolder(time.Hour)
	e.users.EXPECT().SpendCredits(ctx, int64(7), 2, "Web").Return(nil)
	e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(nil)
	e.folders.EXPECT().SaveFiles(ctx, int64(1000), gomock.Any(), gomock.Any()).Return(errBoom)
	rs, err := e.ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, Agent: "Web", FolderID: 1000, Files: []entity.FileUpload{{Path: "a"}}})
	require.NoError(t, err)
	assert.Len(t, rs.Files, 1)
	assert.Contains(t, buf.String(), "record files")
}

// An upload to a path already in the folder is an update: the file is
// replaced and keeps its URL.
func TestPutFiles_ReplacesAFile(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	local, err := storage.NewLocal(t.TempDir())
	require.NoError(t, err)
	ctrl := New(Params{Folders: e.folders, Users: e.users, Storage: local, Limits: func() entity.FileSettings { return e.limits }, Now: func() time.Time { return _now }})
	e.folders.EXPECT().FindFolderByID(gomock.Any(), int64(1000)).Return(model.Folder{ID: 1000, UserID: 7, ExpiresAt: _now.Add(time.Hour)}, nil).Times(4)
	e.users.EXPECT().SpendCredits(ctx, int64(7), 2, "").Return(nil).Times(2)
	e.folders.EXPECT().SaveFiles(ctx, int64(1000), gomock.Any(), gomock.Any()).Return(nil).Times(2)
	e.folders.EXPECT().ListFiles(ctx, int64(1000)).Return(nil, nil)

	put := func(data string) entity.File {
		rs, err := ctrl.PutFiles(ctx, &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "site/index.html", Data: []byte(data)}}})
		require.NoError(t, err)
		return rs.Files[0]
	}
	first, second := put("v1"), put("version 2")
	assert.Equal(t, first.URL, second.URL)
	assert.Equal(t, int64(9), second.Size)

	rs, err := ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	require.NoError(t, err)
	require.Len(t, rs.Files, 1)
	assert.Equal(t, "site/index.html", rs.Files[0].Path)
	file, err := ctrl.OpenFile(ctx, &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "site/index.html"})
	require.NoError(t, err)
	defer func() { _ = file.Body.Close() }()
	body, err := io.ReadAll(file.Body)
	require.NoError(t, err)
	assert.Equal(t, "version 2", string(body))
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
		e.users.EXPECT().SpendCredits(ctx, int64(7), 2, "").Return(repository.ErrInsufficientCredits)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, ErrInsufficientCredits)
		assert.Contains(t, err.Error(), "2 needed")
	})
	t.Run("spend error", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.users.EXPECT().SpendCredits(ctx, int64(7), 2, "").Return(errBoom)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestPutFiles_WriteFailureRefundsTheRest(t *testing.T) {
	ctx := context.Background()
	// a costs 1, b 2 and c 3 credits.
	rq := &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{
		{Path: "a"}, {Path: "b", Data: make([]byte, 1<<20+1)}, {Path: "c", Data: make([]byte, 2<<20+1)},
	}}

	t.Run("refunded", func(t *testing.T) {
		e := newEnv(t)
		e.limits.MaxUploadMB = 4
		e.liveFolder(30 * time.Minute)
		gomock.InOrder(
			e.users.EXPECT().SpendCredits(ctx, int64(7), 6, "").Return(nil),
			e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(nil),
			e.storage.EXPECT().Put(ctx, _prefix+"b", gomock.Any()).Return(errBoom),
			e.users.EXPECT().RefundCredits(ctx, int64(7), 5, "").Return(nil),
			// a was written, so it is recorded.
			e.folders.EXPECT().SaveFiles(ctx, int64(1000), "", model.FolderFiles{"a": {ContentType: "application/octet-stream", UpdatedAt: _now}}).Return(nil),
		)
		_, err := e.ctrl.PutFiles(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.Contains(t, err.Error(), "write b")
	})
	t.Run("refund fails too", func(t *testing.T) {
		e := newEnv(t)
		e.limits.MaxUploadMB = 4
		e.liveFolder(30 * time.Minute)
		refundErr := errors.New("refund failed")
		e.users.EXPECT().SpendCredits(ctx, int64(7), 6, "").Return(nil)
		e.storage.EXPECT().Put(ctx, _prefix+"a", gomock.Any()).Return(errBoom)
		e.users.EXPECT().RefundCredits(ctx, int64(7), 6, "").Return(refundErr)
		e.folders.EXPECT().SaveFiles(ctx, int64(1000), "", model.FolderFiles{}).Return(nil)
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
	e.folders.EXPECT().DeleteFile(ctx, int64(1000), "site/a.txt").Return(nil)
	require.NoError(t, e.ctrl.DeleteFile(ctx, rq))

	e.liveFolder(time.Hour)
	e.storage.EXPECT().Delete(ctx, _prefix+"site/a.txt").Return(nil)
	e.folders.EXPECT().DeleteFile(ctx, int64(1000), "site/a.txt").Return(errBoom)
	assert.ErrorIs(t, e.ctrl.DeleteFile(ctx, rq), errBoom)

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

func TestOpenPublicFile_MarkdownAsHTML(t *testing.T) {
	ctx := context.Background()
	open := func(t *testing.T, path, format string, body io.ReadCloser) (*entity.OpenFileResponse, error) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.storage.EXPECT().Get(ctx, _prefix+path).Return(body, storage.Object{Size: 9, ModTime: _now}, nil)
		return e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000, Path: path, Format: format})
	}

	t.Run("rendered", func(t *testing.T) {
		rs, err := open(t, "docs/README.md", "html", io.NopCloser(strings.NewReader("# Hello\n")))
		require.NoError(t, err)
		page, _ := io.ReadAll(rs.Body)
		assert.NoError(t, rs.Body.Close())
		assert.Equal(t, "text/html; charset=utf-8", rs.ContentType)
		assert.Equal(t, int64(len(page)), rs.Size)
		assert.Equal(t, _now, rs.ModTime)
		assert.Contains(t, string(page), "<title>Hello</title>")
	})
	t.Run("not markdown", func(t *testing.T) {
		body := io.NopCloser(strings.NewReader("x"))
		rs, err := open(t, "a.txt", "html", body)
		require.NoError(t, err)
		assert.Equal(t, body, rs.Body)
	})
	t.Run("raw without the format", func(t *testing.T) {
		body := io.NopCloser(strings.NewReader("# Hello"))
		rs, err := open(t, "a.md", "", body)
		require.NoError(t, err)
		assert.Equal(t, body, rs.Body)
	})
	t.Run("read fails", func(t *testing.T) {
		_, err := open(t, "a.md", "html", io.NopCloser(iotest.ErrReader(errBoom)))
		assert.ErrorIs(t, err, errBoom)
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
	assert.Equal(t, "000000rs", FolderHost(1000))
}

func TestDeleteExpiredFolders(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.folders.EXPECT().ListExpiredFolders(ctx, _now, _sweepBatch).Return([]model.Folder{
		{ID: 1, UserID: 7}, {ID: 2, UserID: 8}, {ID: 3, UserID: 9},
	}, nil)
	e.storage.EXPECT().DeletePrefix(ctx, "u7/00000001/").Return(nil)
	e.folders.EXPECT().DeleteFolder(ctx, int64(1)).Return(nil)
	e.storage.EXPECT().DeletePrefix(ctx, "u8/00000002/").Return(errBoom)
	e.storage.EXPECT().DeletePrefix(ctx, "u9/00000003/").Return(nil)
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

// signingStorage is a storage whose bucket signs URLs.
type signingStorage struct {
	*mockstorage.MockService
	*mockstorage.MockSigner
}

// newSigningEnv is newEnv on a signing storage that redirects files over
// 1 MB.
func newSigningEnv(t *testing.T) (*env, *mockstorage.MockSigner) {
	t.Helper()
	e := newEnv(t)
	signer := mockstorage.NewMockSigner(gomock.NewController(t))
	e.ctrl = New(Params{
		Folders: e.folders, Users: e.users, Storage: signingStorage{e.storage, signer},
		Limits: func() entity.FileSettings { return e.limits }, Now: func() time.Time { return _now },
		RedirectOver: 1 << 20, RedirectTTL: 15 * time.Minute,
	})
	return e, signer
}

// folderWithFiles expects a lookup of folder 1000, owned by user 7, that
// lists files.
func (e *env) folderWithFiles(files model.FolderFiles) {
	e.folders.EXPECT().FindFolderByID(gomock.Any(), int64(1000)).
		Return(model.Folder{ID: 1000, UserID: 7, ExpiresAt: _now.Add(time.Hour), Files: files}, nil)
}

// A folder that lists its files is listed without asking the storage.
func TestFolderFiles_ListedWithoutStorage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	files := model.FolderFiles{
		"b.png":    {Size: 2, ContentType: "image/png", UpdatedAt: _now},
		"a/c.html": {Size: 1, ContentType: "text/html; charset=utf-8", UpdatedAt: _now.Add(-time.Minute)},
	}
	e.folderWithFiles(files)
	e.folders.EXPECT().ListFiles(ctx, int64(1000)).Return([]model.File{{Path: "b.png", Agent: "Web"}}, nil)
	rs, err := e.ctrl.GetFolder(ctx, &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	require.NoError(t, err)
	assert.Equal(t, []entity.File{
		{Path: "a/c.html", Size: 1, UpdatedAt: _now.Add(-time.Minute), URL: _host + "a/c.html"},
		{Path: "b.png", Size: 2, UpdatedAt: _now, URL: _host + "b.png", Agent: "Web"},
	}, rs.Files, "sorted by path")

	e.folders.EXPECT().ListFoldersByUser(ctx, int64(7), _now).
		Return([]model.Folder{{ID: 1000, UserID: 7, ExpiresAt: _now, Files: model.FolderFiles{"only.txt": {Size: 1}}}}, nil)
	list, err := e.ctrl.ListFolders(ctx, &entity.ListFoldersRequest{UserID: 7})
	require.NoError(t, err)
	assert.Equal(t, "only.txt", list.Folders[0].DefaultTitle)

	e.folderWithFiles(model.FolderFiles{"big.bin": {Size: 3 << 20}})
	e.users.EXPECT().SpendCredits(ctx, int64(7), 3, "").Return(nil)
	e.folders.EXPECT().ExtendFolder(ctx, int64(1000), _now, _now.Add(90*time.Minute)).Return(nil)
	ext, err := e.ctrl.ExtendFolder(ctx, &entity.ExtendFolderRequest{UserID: 7, FolderID: 1000, Minutes: 30})
	require.NoError(t, err)
	assert.Equal(t, 3, ext.CreditsSpent, "3 MB x 1 block, sized from the list")
}

func TestOpenFile_Redirects(t *testing.T) {
	ctx := context.Background()
	big := model.FolderFile{Size: 1<<20 + 1, ContentType: "application/octet-stream", UpdatedAt: _now}
	files := model.FolderFiles{"dir/big file.bin": big, "small.bin": {Size: 1 << 20}}
	rq := &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "dir/big file.bin", Redirect: true}
	sign := storage.SignOptions{
		TTL: 15 * time.Minute, ContentType: "application/octet-stream", ContentDisposition: `attachment; filename="big file.bin"`,
	}

	t.Run("over 1 MB", func(t *testing.T) {
		e, signer := newSigningEnv(t)
		e.folderWithFiles(files)
		signer.EXPECT().SignGet(ctx, _prefix+"dir/big file.bin", sign).Return("https://bucket/signed", nil)
		rs, err := e.ctrl.OpenFile(ctx, rq)
		require.NoError(t, err)
		assert.Equal(t, &entity.OpenFileResponse{RedirectURL: "https://bucket/signed", Size: 1<<20 + 1, ContentType: "application/octet-stream", ModTime: _now}, rs)
	})
	t.Run("still in memory", func(t *testing.T) {
		e, signer := newSigningEnv(t)
		e.folderWithFiles(files)
		signer.EXPECT().SignGet(ctx, gomock.Any(), gomock.Any()).Return("", nil)
		body := io.NopCloser(strings.NewReader("x"))
		e.storage.EXPECT().Get(ctx, _prefix+"dir/big file.bin").Return(body, storage.Object{Size: 1<<20 + 1}, nil)
		rs, err := e.ctrl.OpenFile(ctx, rq)
		require.NoError(t, err)
		assert.Equal(t, body, rs.Body)
		assert.Empty(t, rs.RedirectURL)
	})
	t.Run("signing fails", func(t *testing.T) {
		e, signer := newSigningEnv(t)
		e.folderWithFiles(files)
		signer.EXPECT().SignGet(ctx, gomock.Any(), gomock.Any()).Return("", errBoom)
		_, err := e.ctrl.OpenFile(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	// Streamed: 1 MB exactly, a file the list doesn't know, a folder
	// without a list, and a caller that can't follow a redirect (MCP).
	for name, tc := range map[string]struct {
		files    model.FolderFiles
		path     string
		redirect bool
	}{
		"1 MB":            {files, "small.bin", true},
		"not listed":      {files, "other.bin", true},
		"folder w/o list": {nil, "dir/big file.bin", true},
		"no redirect":     {files, "dir/big file.bin", false},
	} {
		t.Run(name, func(t *testing.T) {
			e, _ := newSigningEnv(t)
			e.folderWithFiles(tc.files)
			e.storage.EXPECT().Get(ctx, _prefix+tc.path).Return(io.NopCloser(strings.NewReader("x")), storage.Object{Size: 1}, nil)
			rs, err := e.ctrl.OpenFile(ctx, &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: tc.path, Redirect: tc.redirect})
			require.NoError(t, err)
			assert.Empty(t, rs.RedirectURL)
		})
	}
}

func TestOpenPublicFile_Redirects(t *testing.T) {
	ctx := context.Background()
	files := model.FolderFiles{"big.png": {Size: 2 << 20, UpdatedAt: _now}, "big.html": {Size: 2 << 20}}

	e, signer := newSigningEnv(t)
	e.folderWithFiles(files)
	signer.EXPECT().SignGet(ctx, _prefix+"big.png", storage.SignOptions{TTL: 15 * time.Minute, ContentType: "image/png"}).Return("https://bucket/png", nil)
	rs, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000, Path: "big.png"})
	require.NoError(t, err)
	assert.Equal(t, "https://bucket/png", rs.RedirectURL)

	// A page stays on the folder's URL, sandboxed, with working links.
	e.folderWithFiles(files)
	e.storage.EXPECT().Get(ctx, _prefix+"big.html").Return(io.NopCloser(strings.NewReader("x")), storage.Object{Size: 2 << 20}, nil)
	rs, err = e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000, Path: "big.html"})
	require.NoError(t, err)
	assert.Empty(t, rs.RedirectURL)
	assert.Equal(t, "text/html; charset=utf-8", rs.ContentType)

	// Markdown read as a page is rendered here from its bytes; as a file
	// it redirects like any other.
	files = model.FolderFiles{"big.md": {Size: 2 << 20}}
	e.folderWithFiles(files)
	e.storage.EXPECT().Get(ctx, _prefix+"big.md").Return(io.NopCloser(strings.NewReader("# Hi")), storage.Object{Size: 2 << 20}, nil)
	rs, err = e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000, Path: "big.md", Format: markdown.FormatHTML})
	require.NoError(t, err)
	assert.Empty(t, rs.RedirectURL)
	assert.Equal(t, markdown.ContentType, rs.ContentType)
	e.folderWithFiles(files)
	signer.EXPECT().SignGet(ctx, _prefix+"big.md", gomock.Any()).Return("https://bucket/md", nil)
	rs, err = e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000, Path: "big.md"})
	require.NoError(t, err)
	assert.Equal(t, "https://bucket/md", rs.RedirectURL)
}
