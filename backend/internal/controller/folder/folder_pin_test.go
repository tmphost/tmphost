// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package folder

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/pquerna/otp/totp"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/pin"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/storage"
	mocktoken "github.com/tmphost/tmphost/backend/internal/service/token/mocks"
)

// _pinHash is the hash of PIN 123456, made once: bcrypt is slow.
var _pinHash = pin.Hash("123456")

// pinnedFolder expects a lookup of folder 1000, owned by user 7, with its
// own PIN hash.
func (e *env) pinnedFolder(hash string) model.Folder {
	f := model.Folder{ID: 1000, UserID: 7, CreatedAt: _now.Add(-time.Minute), ExpiresAt: _now.Add(time.Hour), PinHash: hash, Files: model.FolderFiles{}}
	e.folders.EXPECT().FindFolderByID(gomock.Any(), int64(1000)).Return(f, nil)
	return f
}

// expectFile expects one read of the folder's index.html.
func (e *env) expectFile() {
	e.storage.EXPECT().Get(gomock.Any(), _prefix+"index.html").Return(io.NopCloser(strings.NewReader("x")), storage.Object{Size: 1, ModTime: _now}, nil)
}

func TestCreateFolder_Pins(t *testing.T) {
	ctx := context.Background()
	create := func(t *testing.T, e *env, rq *entity.CreateFolderRequest) (entity.Folder, model.Folder) {
		t.Helper()
		var stored model.Folder
		e.folders.EXPECT().CreateFolder(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, f model.Folder) (model.Folder, error) {
			stored = f
			return f, nil
		})
		rs, err := e.ctrl.CreateFolder(ctx, rq)
		require.NoError(t, err)
		return rs.Folder, stored
	}

	t.Run("on request", func(t *testing.T) {
		e := newEnv(t)
		f, stored := create(t, e, &entity.CreateFolderRequest{UserID: 7, Agent: "Web", Pin: true})
		assert.True(t, pin.Valid(f.Pin), f.Pin)
		assert.True(t, pin.Match(stored.PinHash, f.Pin), "only the hash is stored")
		assert.Equal(t, entity.PinModeFolder, f.PinMode)
	})
	t.Run("none by default", func(t *testing.T) {
		e := newEnv(t)
		f, stored := create(t, e, &entity.CreateFolderRequest{UserID: 7, Agent: "Claude Code"})
		assert.Empty(t, f.Pin)
		assert.Empty(t, f.PinMode)
		assert.Empty(t, stored.PinHash)
	})
	t.Run("agent pins", func(t *testing.T) {
		e := newEnv(t)
		e.owner.AgentPins = true
		f, stored := create(t, e, &entity.CreateFolderRequest{UserID: 7, Agent: "Claude Code"})
		assert.True(t, pin.Match(stored.PinHash, f.Pin))
		f, _ = create(t, e, &entity.CreateFolderRequest{UserID: 7, Agent: "Web"})
		assert.Empty(t, f.Pin, "not for the web app")
	})
	t.Run("anonymous on request", func(t *testing.T) {
		e := newEnv(t)
		e.ownerErr = repository.ErrNotFound
		anon := idgen.AnonymousUserID("10.0.0.1")
		f, _ := create(t, e, &entity.CreateFolderRequest{UserID: anon, Anonymous: true, Agent: "Web", Pin: true})
		assert.True(t, pin.Valid(f.Pin), "no account, so no settings")
	})
	t.Run("account pin guards it instead", func(t *testing.T) {
		e := newEnv(t)
		e.owner.PinMode, e.owner.AgentPins = model.PinModeStatic, true
		f, stored := create(t, e, &entity.CreateFolderRequest{UserID: 7, Agent: "Claude Code", Pin: true})
		assert.Empty(t, f.Pin)
		assert.Empty(t, stored.PinHash)
		assert.Equal(t, entity.PinModeStatic, f.PinMode)
	})
	t.Run("owner lookup fails", func(t *testing.T) {
		e := newEnv(t)
		e.ownerErr = errBoom
		_, err := e.ctrl.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: 7})
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("unknown owner has no settings", func(t *testing.T) {
		e := newEnv(t)
		e.ownerErr = repository.ErrNotFound
		f, _ := create(t, e, &entity.CreateFolderRequest{UserID: 7})
		assert.Empty(t, f.PinMode)
	})
}

func TestListFolders_PinModes(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.owner.PinMode = model.PinModeTOTP
	e.folders.EXPECT().ListFoldersByUser(ctx, int64(7), _now).Return([]model.Folder{{ID: 1000, UserID: 7, Files: model.FolderFiles{}}}, nil)
	rs, err := e.ctrl.ListFolders(ctx, &entity.ListFoldersRequest{UserID: 7})
	require.NoError(t, err)
	assert.Equal(t, entity.PinModeTOTP, rs.Folders[0].PinMode)

	e.ownerErr = errBoom
	_, err = e.ctrl.ListFolders(ctx, &entity.ListFoldersRequest{UserID: 7})
	assert.ErrorIs(t, err, errBoom)
}

func TestGetFolder_OwnerLookupFails(t *testing.T) {
	e := newEnv(t)
	e.ownerErr = errBoom
	e.liveFolder(time.Hour)
	_, err := e.ctrl.GetFolder(context.Background(), &entity.GetFolderRequest{UserID: 7, FolderID: 1000})
	assert.ErrorIs(t, err, errBoom)
}

func TestCreateFolderPin(t *testing.T) {
	ctx := context.Background()
	rq := &entity.FolderPinRequest{UserID: 7, FolderID: 1000}

	t.Run("made", func(t *testing.T) {
		e := newEnv(t)
		e.pinnedFolder("")
		var hash string
		e.folders.EXPECT().UpdateFolderPin(ctx, int64(1000), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, h string) error {
			hash = h
			return nil
		})
		rs, err := e.ctrl.CreateFolderPin(ctx, rq)
		require.NoError(t, err)
		assert.True(t, pin.Match(hash, rs.Folder.Pin))
		assert.Equal(t, entity.PinModeFolder, rs.Folder.PinMode)
	})
	t.Run("account pin set", func(t *testing.T) {
		e := newEnv(t)
		e.owner.PinMode = model.PinModeTOTP
		e.pinnedFolder("")
		_, err := e.ctrl.CreateFolderPin(ctx, rq)
		assert.ErrorIs(t, err, ErrAccountPin)
	})
	t.Run("not found", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, repository.ErrNotFound)
		_, err := e.ctrl.CreateFolderPin(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("gone while updating", func(t *testing.T) {
		e := newEnv(t)
		e.pinnedFolder("")
		e.folders.EXPECT().UpdateFolderPin(ctx, int64(1000), gomock.Any()).Return(repository.ErrNotFound)
		_, err := e.ctrl.CreateFolderPin(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("update fails", func(t *testing.T) {
		e := newEnv(t)
		e.pinnedFolder("")
		e.folders.EXPECT().UpdateFolderPin(ctx, int64(1000), gomock.Any()).Return(errBoom)
		_, err := e.ctrl.CreateFolderPin(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("listing fails", func(t *testing.T) {
		e := newEnv(t)
		e.liveFolder(time.Hour)
		e.folders.EXPECT().UpdateFolderPin(ctx, int64(1000), gomock.Any()).Return(nil)
		e.storage.EXPECT().List(ctx, _prefix).Return(nil, errBoom)
		_, err := e.ctrl.CreateFolderPin(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestDeleteFolderPin(t *testing.T) {
	ctx := context.Background()
	rq := &entity.FolderPinRequest{UserID: 7, FolderID: 1000}

	e := newEnv(t)
	e.pinnedFolder(_pinHash)
	e.folders.EXPECT().UpdateFolderPin(ctx, int64(1000), "").Return(nil)
	rs, err := e.ctrl.DeleteFolderPin(ctx, rq)
	require.NoError(t, err)
	assert.Empty(t, rs.Folder.PinMode)
	assert.Empty(t, rs.Folder.Pin)

	e.pinnedFolder(_pinHash)
	e.folders.EXPECT().UpdateFolderPin(ctx, int64(1000), "").Return(errBoom)
	_, err = e.ctrl.DeleteFolderPin(ctx, rq)
	assert.ErrorIs(t, err, errBoom)

	e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, repository.ErrNotFound)
	_, err = e.ctrl.DeleteFolderPin(ctx, rq)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestOpenPublicFile_FolderPin(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	open := func(rq entity.OpenPublicFileRequest) (*entity.OpenFileResponse, error) {
		rq.FolderID = 1000
		return e.ctrl.OpenPublicFile(ctx, &rq)
	}

	e.pinnedFolder(_pinHash)
	_, err := open(entity.OpenPublicFileRequest{Client: "a"})
	assert.ErrorIs(t, err, ErrPinRequired)

	for range PinAttempts {
		e.pinnedFolder(_pinHash)
		_, err = open(entity.OpenPublicFileRequest{Pin: "000000", Client: "a"})
		assert.ErrorIs(t, err, ErrWrongPin)
	}
	e.pinnedFolder(_pinHash)
	_, err = open(entity.OpenPublicFileRequest{Pin: "123456", Client: "a"})
	assert.ErrorIs(t, err, ErrPinRateLimited, "even the right PIN, until the window ends")

	// Another visitor has attempts of their own, and right PINs don't use
	// them up.
	var unlock *entity.UnlockFolderResponse
	for range PinAttempts + 1 {
		e.pinnedFolder(_pinHash)
		e.expectFile()
		rs, err := open(entity.OpenPublicFileRequest{Pin: "123456", Client: "b"})
		require.NoError(t, err)
		assert.True(t, rs.Private)
		require.NotNil(t, rs.Unlock)
		unlock = rs.Unlock
	}
	assert.Equal(t, _now.Add(time.Hour), unlock.ExpiresAt, "the token lives as long as the folder")

	// The token opens the folder without a PIN.
	e.pinnedFolder(_pinHash)
	e.expectFile()
	rs, err := open(entity.OpenPublicFileRequest{PinToken: unlock.Token, Client: "c"})
	require.NoError(t, err)
	assert.True(t, rs.Private)
	assert.Nil(t, rs.Unlock)

	// A new PIN signs everyone out.
	e.pinnedFolder(pin.Hash("654321"))
	_, err = open(entity.OpenPublicFileRequest{PinToken: unlock.Token, Client: "c"})
	assert.ErrorIs(t, err, ErrPinRequired)

	// It opens no other folder.
	other, err := e.tokens.CreateFolderPinToken(idgen.FormatFolderID(1001), pin.Fingerprint(entity.PinModeFolder, _pinHash), _now.Add(time.Hour))
	require.NoError(t, err)
	e.pinnedFolder(_pinHash)
	_, err = open(entity.OpenPublicFileRequest{PinToken: other, Client: "c"})
	assert.ErrorIs(t, err, ErrPinRequired)

	// A guarded file that does not exist is still not found.
	e.pinnedFolder(_pinHash)
	e.storage.EXPECT().Get(ctx, _prefix+"nope.txt").Return(nil, storage.Object{}, storage.ErrNotFound)
	_, err = open(entity.OpenPublicFileRequest{Path: "nope.txt", PinToken: unlock.Token})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestOpenPublicFile_OpenFolder(t *testing.T) {
	e := newEnv(t)
	e.pinnedFolder("")
	e.expectFile()
	rs, err := e.ctrl.OpenPublicFile(context.Background(), &entity.OpenPublicFileRequest{FolderID: 1000, Pin: "000000"})
	require.NoError(t, err)
	assert.False(t, rs.Private)
	assert.Nil(t, rs.Unlock, "nothing to unlock")
}

func TestOpenPublicFile_StaticAccountPin(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.owner.PinMode, e.owner.PinHash = model.PinModeStatic, _pinHash
	// The account PIN wins over the folder's own.
	e.pinnedFolder(pin.Hash("999999"))
	_, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000, Pin: "999999"})
	assert.ErrorIs(t, err, ErrWrongPin)
	e.pinnedFolder("")
	e.expectFile()
	rs, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000, Pin: "123456"})
	require.NoError(t, err)
	assert.NotNil(t, rs.Unlock)
}

func TestOpenPublicFile_TOTPAccountPin(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	key, err := pin.NewTOTP("me@example.com")
	require.NoError(t, err)
	e.owner.PinMode, e.owner.TOTPSecret = model.PinModeTOTP, pin.SealSecret(e.secrets, 7, key.Secret)
	code, err := totp.GenerateCode(key.Secret, _now)
	require.NoError(t, err)

	e.pinnedFolder("")
	e.expectFile()
	rs, err := e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000, Pin: code})
	require.NoError(t, err)
	assert.NotNil(t, rs.Unlock)

	// A secret that can't be opened opens nothing, and says why.
	var buf bytes.Buffer
	prev := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = prev })
	e.owner.TOTPSecret = "!!"
	e.pinnedFolder("")
	_, err = e.ctrl.OpenPublicFile(ctx, &entity.OpenPublicFileRequest{FolderID: 1000, Pin: code})
	assert.ErrorIs(t, err, ErrWrongPin)
	assert.Contains(t, buf.String(), "open totp secret")
}

func TestOpenPublicFile_PinOwnerLookupFails(t *testing.T) {
	e := newEnv(t)
	e.ownerErr = errBoom
	e.pinnedFolder(_pinHash)
	_, err := e.ctrl.OpenPublicFile(context.Background(), &entity.OpenPublicFileRequest{FolderID: 1000})
	assert.ErrorIs(t, err, errBoom)
}

func TestOpenPublicFile_PinTokenSigningFails(t *testing.T) {
	e := newEnv(t)
	tokens := mocktoken.NewMockService(gomock.NewController(t))
	tokens.EXPECT().ValidateFolderPinToken("").Return("", "", errBoom)
	tokens.EXPECT().CreateFolderPinToken(gomock.Any(), gomock.Any(), gomock.Any()).Return("", errBoom)
	c := e.ctrl.(*controller)
	c.tokens = tokens
	e.pinnedFolder(_pinHash)
	_, err := c.OpenPublicFile(context.Background(), &entity.OpenPublicFileRequest{FolderID: 1000, Pin: "123456"})
	assert.ErrorIs(t, err, errBoom)
}

func TestUnlockFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	e.pinnedFolder(_pinHash)
	rs, err := e.ctrl.UnlockFolder(ctx, &entity.UnlockFolderRequest{UserID: 7, FolderID: 1000, Pin: "123456", Client: "a"})
	require.NoError(t, err)
	folderID, fp, err := e.tokens.ValidateFolderPinToken(rs.Token)
	require.NoError(t, err)
	assert.Equal(t, "000000rs", folderID)
	assert.Equal(t, pin.Fingerprint(entity.PinModeFolder, _pinHash), fp)

	e.pinnedFolder(_pinHash)
	_, err = e.ctrl.UnlockFolder(ctx, &entity.UnlockFolderRequest{FolderID: 1000, Pin: "111111", Client: "a"})
	assert.ErrorIs(t, err, ErrWrongPin)

	e.pinnedFolder(_pinHash)
	_, err = e.ctrl.UnlockFolder(ctx, &entity.UnlockFolderRequest{FolderID: 1000, Client: "a"})
	assert.ErrorIs(t, err, ErrWrongPin, "no PIN is a wrong one")

	e.pinnedFolder("")
	rs, err = e.ctrl.UnlockFolder(ctx, &entity.UnlockFolderRequest{FolderID: 1000, Pin: "123456"})
	require.NoError(t, err)
	assert.Empty(t, rs.Token, "an open folder needs no token")

	e.pinnedFolder(_pinHash)
	_, err = e.ctrl.UnlockFolder(ctx, &entity.UnlockFolderRequest{UserID: 8, FolderID: 1000, Pin: "123456"})
	assert.ErrorIs(t, err, ErrNotFound, "someone else's folder")

	e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, repository.ErrNotFound)
	_, err = e.ctrl.UnlockFolder(ctx, &entity.UnlockFolderRequest{FolderID: 1000, Pin: "123456"})
	assert.ErrorIs(t, err, ErrNotFound)

	e.ownerErr = errBoom
	e.pinnedFolder(_pinHash)
	_, err = e.ctrl.UnlockFolder(ctx, &entity.UnlockFolderRequest{FolderID: 1000, Pin: "123456"})
	assert.ErrorIs(t, err, errBoom)
}
