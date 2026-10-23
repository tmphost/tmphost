// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package folder implements temporary file hosting: users upload files
// into folders that expire after a TTL, and every file is served at a
// public URL until then.
//
// Files live in storage under u<userID>/<folderID>/<path>; the database
// only records each folder's owner and expiry. A folder is served below
// the app, at /files/<userID>/<folderID>/, or with subdomains on, on its
// own origin, <folderID>.<public domain>. Both IDs are base36 because
// hostnames are case-insensitive; folder IDs are random and always 11
// characters (idgen.FormatFolderID). Each file write costs one
// credit per started BillingUnit of its size per started BillingBlock of
// the folder's remaining lifetime.
//
// Anonymous web visitors (idgen.AnonymousUserID) pay no credits. They have
// lower limits instead, and a per-IP quota of file writes and new folders.
package folder

//go:generate mockgen -source=folder.go -destination=mocks/folder.go -package=mocks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	zlog "github.com/rs/zerolog/log"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/markdown"
	"github.com/tmphost/tmphost/backend/internal/pin"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/quota"
	"github.com/tmphost/tmphost/backend/internal/service/storage"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

const (
	// DefaultTTL is a folder's lifetime when none is asked for.
	DefaultTTL = 30 * time.Minute
	// BillingBlock is the lifetime one credit pays for, per BillingUnit.
	BillingBlock = 30 * time.Minute
	// BillingUnit is the file size one credit pays for, per BillingBlock.
	BillingUnit = 1 << 20
	// DefaultMaxTTLMinutes is the longest TTL unless root changes it.
	DefaultMaxTTLMinutes = 7200 * 60
	// DefaultMaxUploadMB caps an upload request unless root changes it.
	DefaultMaxUploadMB = 16
	// SweepSchedule is the cron spec of the job that deletes expired
	// folders.
	SweepSchedule = "* * * * *"
	// MaxDirDepth is how many directories deep a file can be in a folder.
	MaxDirDepth = 5
	// PublicPathPrefix is where folders are served on the app origin when
	// subdomains are off: <base URL>/files/<userID>/<folderID>/<path>.
	PublicPathPrefix = "/files/"

	// DefaultAnonymousMaxTTLMinutes is the longest lifetime of an
	// anonymous folder unless root changes it.
	DefaultAnonymousMaxTTLMinutes = 10
	// AnonymousMaxUploadMB caps each file an anonymous visitor uploads.
	AnonymousMaxUploadMB = 1
	// AnonymousOps is how many files an anonymous visitor may write, and
	// how many folders they may create, per AnonymousWindow.
	AnonymousOps = 5
	// AnonymousBytes is how many bytes an anonymous visitor may upload
	// per AnonymousWindow.
	AnonymousBytes = 4 << 20
	// AnonymousWindow is the window of the anonymous quota.
	AnonymousWindow = 30 * time.Minute

	// MaxTitleLength is how many characters a folder title can have.
	MaxTitleLength = 100

	// PinAttempts is how many wrong PINs a visitor may enter for a folder
	// per PinWindow; past them, every PIN is refused until the window
	// ends.
	PinAttempts = 2
	// PinWindow is the window of PinAttempts.
	PinWindow = time.Minute

	_maxPathLength    = 512
	_maxSegmentLength = 255
	_indexFile        = "index.html"
	_sweepBatch       = 500
	// Local storage writes uploads to temp files with this prefix.
	_reservedPrefix = ".upload-"
)

var (
	// ErrNotFound is returned for a folder or file that does not exist,
	// has expired, or belongs to someone else.
	ErrNotFound = errors.New("not found")
	// ErrInvalidTTL is returned for a TTL out of range.
	ErrInvalidTTL = errors.New("invalid ttl")
	// ErrInvalidPath is returned for a file path that is not a clean,
	// relative, slash-separated path at most MaxDirDepth directories deep.
	ErrInvalidPath = errors.New("invalid file path")
	// ErrInvalidTitle is returned for a folder title that is too long or
	// has control characters.
	ErrInvalidTitle = errors.New("invalid folder title")
	// ErrNoFiles is returned for an upload without files.
	ErrNoFiles = errors.New("no files to upload")
	// ErrDuplicatePath is returned when an upload names a path twice.
	ErrDuplicatePath = errors.New("duplicate file path")
	// ErrTooLarge is returned for an upload over the size limit.
	ErrTooLarge = errors.New("upload too large")
	// ErrInsufficientCredits is returned when the user cannot pay for an
	// upload.
	ErrInsufficientCredits = errors.New("insufficient credits")
	// ErrQuotaExceeded is returned when an anonymous visitor has used up
	// their quota for the current window.
	ErrQuotaExceeded = errors.New("anonymous quota exceeded")
	// ErrPinRequired is returned for a PIN-guarded folder when the visitor
	// has neither entered its PIN before nor passed one.
	ErrPinRequired = errors.New("pin required")
	// ErrWrongPin is returned for a PIN that does not open the folder.
	ErrWrongPin = errors.New("wrong pin")
	// ErrPinRateLimited is returned once a visitor has entered
	// PinAttempts wrong PINs in the current PinWindow.
	ErrPinRateLimited = errors.New("too many wrong pins")
	// ErrAccountPin is returned for a folder PIN asked for while the
	// owner's account PIN guards every folder.
	ErrAccountPin = errors.New("the account pin guards this folder")
)

type (
	// Controller is the file hosting business logic.
	Controller interface {
		GetLimits(ctx context.Context, rq *entity.GetLimitsRequest) *entity.GetLimitsResponse
		CreateFolder(ctx context.Context, rq *entity.CreateFolderRequest) (*entity.CreateFolderResponse, error)
		ListFolders(ctx context.Context, rq *entity.ListFoldersRequest) (*entity.ListFoldersResponse, error)
		GetFolder(ctx context.Context, rq *entity.GetFolderRequest) (*entity.GetFolderResponse, error)
		// UpdateFolder renames a folder; an empty title clears it.
		UpdateFolder(ctx context.Context, rq *entity.UpdateFolderRequest) (*entity.UpdateFolderResponse, error)
		// ExtendFolder makes a folder expire later, charging each of its
		// files Cost for the added time.
		ExtendFolder(ctx context.Context, rq *entity.ExtendFolderRequest) (*entity.ExtendFolderResponse, error)
		// CreateFolderPin gives a folder a new PIN of its own, replacing
		// any it had; ErrAccountPin while the owner's account PIN is set.
		CreateFolderPin(ctx context.Context, rq *entity.FolderPinRequest) (*entity.FolderPinResponse, error)
		// DeleteFolderPin removes a folder's own PIN.
		DeleteFolderPin(ctx context.Context, rq *entity.FolderPinRequest) (*entity.FolderPinResponse, error)
		DeleteFolder(ctx context.Context, rq *entity.DeleteFolderRequest) error
		PutFiles(ctx context.Context, rq *entity.PutFilesRequest) (*entity.PutFilesResponse, error)
		DeleteFile(ctx context.Context, rq *entity.DeleteFileRequest) error
		// OpenFile opens one of a user's files.
		OpenFile(ctx context.Context, rq *entity.OpenFileRequest) (*entity.OpenFileResponse, error)
		// OpenPublicFile opens a file of a live folder for anyone who may
		// see it: anyone for an open folder, else whoever passes its PIN or
		// a token UnlockFolder gave them.
		OpenPublicFile(ctx context.Context, rq *entity.OpenPublicFileRequest) (*entity.OpenFileResponse, error)
		// UnlockFolder checks a folder's PIN and returns the token that
		// proves it was entered.
		UnlockFolder(ctx context.Context, rq *entity.UnlockFolderRequest) (*entity.UnlockFolderResponse, error)
		// DeleteExpiredFolders removes a batch of expired folders, files
		// first, so a failure leaves the record for the next run.
		DeleteExpiredFolders(ctx context.Context) (*entity.DeleteExpiredFoldersResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Folders repository.FolderRepository
		Users   repository.UserRepository
		Storage storage.Service
		// Anonymous counts anonymous file writes and new folders, per user;
		// AnonymousOps per AnonymousWindow.
		Anonymous quota.Service
		// AnonymousBytes counts the bytes anonymous users upload;
		// AnonymousBytes per AnonymousWindow.
		AnonymousBytes quota.Service
		// PinAttempts counts wrong PINs per folder and visitor;
		// PinAttempts per PinWindow.
		PinAttempts quota.Service
		// Tokens signs the tokens that prove a visitor entered a folder's
		// PIN.
		Tokens token.Service
		// Secrets decrypts the owners' authenticator secrets.
		Secrets pin.Sealer
		// NewID returns a new folder ID; idgen.NewFolderID when nil.
		NewID func() int64
		// Limits returns the current file settings; they can change at
		// runtime.
		Limits func() entity.FileSettings
		// BaseURL is the app's public URL. Folders are served below it,
		// or on its scheme with subdomains.
		BaseURL string
		// RedirectOver is the size in bytes past which a file a caller
		// can be redirected for is answered with a URL signed by the
		// storage, valid for RedirectTTL, when the storage signs URLs. 0
		// streams every file.
		RedirectOver int64
		RedirectTTL  time.Duration
		Now          func() time.Time
	}

	controller struct {
		folders   repository.FolderRepository
		users     repository.UserRepository
		storage   storage.Service
		anonymous quota.Service
		anonBytes quota.Service
		pins      quota.Service
		tokens    token.Service
		secrets   pin.Sealer
		newID     func() int64
		limits    func() entity.FileSettings
		baseURL   string
		scheme    string
		redirect  int64
		ttl       time.Duration
		now       func() time.Time
	}
)

// New returns the folder controller.
func New(p Params) Controller {
	c := &controller{
		folders:   p.Folders,
		users:     p.Users,
		storage:   p.Storage,
		anonymous: p.Anonymous,
		anonBytes: p.AnonymousBytes,
		pins:      p.PinAttempts,
		tokens:    p.Tokens,
		secrets:   p.Secrets,
		newID:     p.NewID,
		limits:    p.Limits,
		baseURL:   strings.TrimRight(p.BaseURL, "/"),
		scheme:    "http",
		redirect:  p.RedirectOver,
		ttl:       p.RedirectTTL,
		now:       p.Now,
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.newID == nil {
		c.newID = idgen.NewFolderID
	}
	if u, err := url.Parse(p.BaseURL); err == nil && u.Scheme != "" {
		c.scheme = u.Scheme
	}
	return c
}

// Cost is the credits writing a file of size bytes into a folder with
// remaining lifetime left costs: one per started BillingUnit per started
// BillingBlock. Empty files and expiring folders still cost one of each.
func Cost(size int64, remaining time.Duration) int {
	units := int((size + BillingUnit - 1) / BillingUnit)
	blocks := int((remaining + BillingBlock - 1) / BillingBlock)
	return max(units, 1) * max(blocks, 1)
}

func (c *controller) GetLimits(_ context.Context, rq *entity.GetLimitsRequest) *entity.GetLimitsResponse {
	maxTTL, maxUploadMB := c.maxTTLMinutes(rq.Anonymous), c.maxUploadMB(rq.Anonymous)
	rs := &entity.GetLimitsResponse{
		DefaultTTLMinutes:   min(int(DefaultTTL/time.Minute), maxTTL),
		MaxTTLMinutes:       maxTTL,
		MaxUploadBytes:      int64(maxUploadMB) << 20,
		BillingBlockMinutes: int(BillingBlock / time.Minute),
		BillingUnitBytes:    BillingUnit,
		Anonymous:           rq.Anonymous,
	}
	if rq.Anonymous {
		rs.FileOps, rs.FileOpsWindowMinutes = AnonymousOps, int(AnonymousWindow/time.Minute)
		rs.FileBytes = AnonymousBytes
	}
	return rs
}

func (c *controller) CreateFolder(ctx context.Context, rq *entity.CreateFolderRequest) (*entity.CreateFolderResponse, error) {
	maxTTL := c.maxTTLMinutes(rq.Anonymous)
	ttl := time.Duration(rq.TTLMinutes) * time.Minute
	if rq.TTLMinutes == 0 {
		ttl = min(DefaultTTL, time.Duration(maxTTL)*time.Minute)
	}
	if ttl < time.Minute || ttl > time.Duration(maxTTL)*time.Minute {
		return nil, fmt.Errorf("%w: must be between 1 and %d minutes", ErrInvalidTTL, maxTTL)
	}
	owner, err := c.owner(ctx, rq.UserID)
	if err != nil {
		return nil, err
	}
	if rq.Anonymous && !c.anonymous.Take(folderQuotaKey(rq.UserID), 1) {
		return nil, fmt.Errorf("%w: at most %d new folders per %d minutes", ErrQuotaExceeded, AnonymousOps, int(AnonymousWindow/time.Minute))
	}
	now := c.now().UTC()
	m := model.Folder{
		ID:        c.newID(),
		CreatedAt: now,
		UserID:    rq.UserID,
		ExpiresAt: now.Add(ttl),
		Agent:     rq.Agent,
		Files:     model.FolderFiles{},
	}
	// An account PIN guards every folder; a folder's own PIN is only
	// made without one: on request, or for agents when the owner asked.
	var clear string
	if owner.PinMode == model.PinModeOff && (rq.Pin || (owner.AgentPins && !rq.Anonymous && rq.Agent != authctrl.AgentWeb)) {
		clear = pin.Generate()
		m.PinHash = pin.Hash(clear)
	}
	f, err := c.folders.CreateFolder(ctx, m)
	if err != nil {
		if rq.Anonymous {
			c.anonymous.Give(folderQuotaKey(rq.UserID), 1)
		}
		return nil, fmt.Errorf("create folder: %w", err)
	}
	folder := c.folderEntity(f, owner)
	folder.Pin = clear
	return &entity.CreateFolderResponse{Folder: folder}, nil
}

func (c *controller) ListFolders(ctx context.Context, rq *entity.ListFoldersRequest) (*entity.ListFoldersResponse, error) {
	owner, err := c.owner(ctx, rq.UserID)
	if err != nil {
		return nil, err
	}
	folders, err := c.folders.ListFoldersByUser(ctx, rq.UserID, c.now())
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	out := make([]entity.Folder, len(folders))
	for i, f := range folders {
		out[i] = c.folderEntity(f, owner)
		// A folder whose files cannot be listed still shows, under its ID.
		if objects, err := c.objects(ctx, f); err == nil {
			c.describe(&out[i], f, objects)
		}
	}
	return &entity.ListFoldersResponse{Folders: out}, nil
}

func (c *controller) GetFolder(ctx context.Context, rq *entity.GetFolderRequest) (*entity.GetFolderResponse, error) {
	f, owner, err := c.ownedFolderAndOwner(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	prefix := folderPrefix(f)
	objects, err := c.objects(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	records, err := c.folders.ListFiles(ctx, f.ID)
	if err != nil {
		return nil, fmt.Errorf("list file records: %w", err)
	}
	agents := make(map[string]string, len(records))
	for _, r := range records {
		agents[r.Path] = r.Agent
	}
	files := make([]entity.File, len(objects))
	for i, o := range objects {
		p := strings.TrimPrefix(o.Key, prefix)
		files[i] = entity.File{Path: p, Size: o.Size, UpdatedAt: o.ModTime.UTC(), URL: c.fileURL(f, p), Agent: agents[p]}
	}
	folder := c.folderEntity(f, owner)
	c.describe(&folder, f, objects)
	return &entity.GetFolderResponse{Folder: folder, Files: files}, nil
}

func (c *controller) UpdateFolder(ctx context.Context, rq *entity.UpdateFolderRequest) (*entity.UpdateFolderResponse, error) {
	title := strings.TrimSpace(rq.Title)
	if !ValidTitle(title) {
		return nil, fmt.Errorf("%w: at most %d characters, no control characters", ErrInvalidTitle, MaxTitleLength)
	}
	f, owner, err := c.ownedFolderAndOwner(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	if err := c.folders.UpdateFolderTitle(ctx, f.ID, title); err != nil {
		return nil, fmt.Errorf("update folder: %w", err)
	}
	f.Title = title
	objects, err := c.objects(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	folder := c.folderEntity(f, owner)
	c.describe(&folder, f, objects)
	return &entity.UpdateFolderResponse{Folder: folder}, nil
}

func (c *controller) ExtendFolder(ctx context.Context, rq *entity.ExtendFolderRequest) (*entity.ExtendFolderResponse, error) {
	f, owner, err := c.ownedFolderAndOwner(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	now := c.now()
	extension := time.Duration(rq.Minutes) * time.Minute
	maxTTL := c.maxTTLMinutes(false)
	if rq.Minutes < 1 || f.ExpiresAt.Add(extension).Sub(now) > time.Duration(maxTTL)*time.Minute {
		return nil, fmt.Errorf("%w: the folder can live at most %d more minutes", ErrInvalidTTL, maxTTL)
	}
	objects, err := c.objects(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	total := 0
	for _, o := range objects {
		total += Cost(o.Size, extension)
	}
	if total > 0 {
		err = c.users.SpendCredits(ctx, rq.UserID, total, rq.Agent)
		if errors.Is(err, repository.ErrInsufficientCredits) {
			return nil, fmt.Errorf("%w: %d needed", ErrInsufficientCredits, total)
		}
		if err != nil {
			return nil, fmt.Errorf("spend credits: %w", err)
		}
	}
	f.ExpiresAt = f.ExpiresAt.Add(extension)
	if err := c.folders.ExtendFolder(ctx, f.ID, now, f.ExpiresAt); err != nil {
		// The time was not added, so it is not paid for.
		if total > 0 {
			if rerr := c.users.RefundCredits(ctx, rq.UserID, total, rq.Agent); rerr != nil {
				err = errors.Join(err, fmt.Errorf("refund credits: %w", rerr))
			}
		}
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errors.Join(ErrNotFound, err)
		}
		return nil, fmt.Errorf("extend folder: %w", err)
	}
	folder := c.folderEntity(f, owner)
	c.describe(&folder, f, objects)
	return &entity.ExtendFolderResponse{Folder: folder, CreditsSpent: total}, nil
}

// ValidTitle reports whether t can be a folder title: at most
// MaxTitleLength characters of valid UTF-8 with no control characters.
// The empty title is valid; it clears the title.
func ValidTitle(t string) bool {
	if !utf8.ValidString(t) || utf8.RuneCountInString(t) > MaxTitleLength {
		return false
	}
	for _, r := range t {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// describe fills in what e says about the files of f, objects: its
// default title, the name of its only file, else its ID; and the URL to
// open, the folder's own when its root has an index.html, else its only
// file's, else none.
func (c *controller) describe(e *entity.Folder, f model.Folder, objects []storage.Object) {
	prefix := folderPrefix(f)
	switch {
	case slices.ContainsFunc(objects, func(o storage.Object) bool { return o.Key == prefix+_indexFile }):
		e.OpenURL = e.URL
	case len(objects) == 1:
		p := strings.TrimPrefix(objects[0].Key, prefix)
		e.DefaultTitle = path.Base(p)
		e.OpenURL = c.fileURL(f, p)
	}
}

func (c *controller) CreateFolderPin(ctx context.Context, rq *entity.FolderPinRequest) (*entity.FolderPinResponse, error) {
	f, owner, err := c.ownedFolderAndOwner(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	if owner.PinMode != model.PinModeOff {
		return nil, ErrAccountPin
	}
	clear := pin.Generate()
	folder, err := c.setFolderPin(ctx, f, owner, pin.Hash(clear))
	if err != nil {
		return nil, err
	}
	folder.Pin = clear
	return &entity.FolderPinResponse{Folder: folder}, nil
}

func (c *controller) DeleteFolderPin(ctx context.Context, rq *entity.FolderPinRequest) (*entity.FolderPinResponse, error) {
	f, owner, err := c.ownedFolderAndOwner(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	folder, err := c.setFolderPin(ctx, f, owner, "")
	if err != nil {
		return nil, err
	}
	return &entity.FolderPinResponse{Folder: folder}, nil
}

// setFolderPin stores the hash of f's own PIN and describes the folder
// after.
func (c *controller) setFolderPin(ctx context.Context, f model.Folder, owner model.User, hash string) (entity.Folder, error) {
	if err := c.folders.UpdateFolderPin(ctx, f.ID, hash); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return entity.Folder{}, ErrNotFound
		}
		return entity.Folder{}, fmt.Errorf("update folder pin: %w", err)
	}
	f.PinHash = hash
	objects, err := c.objects(ctx, f)
	if err != nil {
		return entity.Folder{}, fmt.Errorf("list files: %w", err)
	}
	folder := c.folderEntity(f, owner)
	c.describe(&folder, f, objects)
	return folder, nil
}

func (c *controller) DeleteFolder(ctx context.Context, rq *entity.DeleteFolderRequest) error {
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return err
	}
	return c.deleteFolder(ctx, f)
}

func (c *controller) PutFiles(ctx context.Context, rq *entity.PutFilesRequest) (*entity.PutFilesResponse, error) {
	if err := c.validateUploads(rq.Files, rq.Anonymous); err != nil {
		return nil, err
	}
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	now := c.now()
	costs := make([]int, len(rq.Files))
	total := 0
	if rq.Anonymous {
		if !c.anonymous.Take(fileQuotaKey(rq.UserID), len(rq.Files)) {
			return nil, fmt.Errorf("%w: at most %d files per %d minutes", ErrQuotaExceeded, AnonymousOps, int(AnonymousWindow/time.Minute))
		}
		if !c.anonBytes.Take(bytesQuotaKey(rq.UserID), uploadSize(rq.Files)) {
			c.anonymous.Give(fileQuotaKey(rq.UserID), len(rq.Files))
			return nil, fmt.Errorf("%w: at most %d MB per %d minutes", ErrQuotaExceeded, AnonymousBytes>>20, int(AnonymousWindow/time.Minute))
		}
	} else {
		for i, u := range rq.Files {
			costs[i] = Cost(int64(len(u.Data)), f.ExpiresAt.Sub(now))
			total += costs[i]
		}
		err = c.users.SpendCredits(ctx, rq.UserID, total, rq.Agent)
		if errors.Is(err, repository.ErrInsufficientCredits) {
			return nil, fmt.Errorf("%w: %d needed", ErrInsufficientCredits, total)
		}
		if err != nil {
			return nil, fmt.Errorf("spend credits: %w", err)
		}
	}

	files := make([]entity.File, 0, len(rq.Files))
	for i, u := range rq.Files {
		if err := c.storage.Put(ctx, folderPrefix(f)+u.Path, u.Data); err != nil {
			// The files not written are not paid for.
			if rerr := c.refund(ctx, rq, i, costs[i:]); rerr != nil {
				err = errors.Join(err, fmt.Errorf("refund credits: %w", rerr))
			}
			c.recordFiles(ctx, f.ID, rq.Agent, files)
			return nil, fmt.Errorf("write %s: %w", u.Path, err)
		}
		files = append(files, entity.File{Path: u.Path, Size: int64(len(u.Data)), UpdatedAt: now.UTC(), URL: c.fileURL(f, u.Path), Agent: rq.Agent})
	}
	c.recordFiles(ctx, f.ID, rq.Agent, files)
	return &entity.PutFilesResponse{Files: files, CreditsSpent: total}, nil
}

// recordFiles records that agent wrote files, in the folder's Files and
// who wrote them. The files are stored and paid for by then, so a failure
// is only logged: they are still served at their URLs, but not listed.
func (c *controller) recordFiles(ctx context.Context, folderID int64, agent string, files []entity.File) {
	records := make(model.FolderFiles, len(files))
	for _, f := range files {
		records[f.Path] = model.FolderFile{Size: f.Size, ContentType: contentType(f.Path), UpdatedAt: f.UpdatedAt}
	}
	if err := c.folders.SaveFiles(ctx, folderID, agent, records); err != nil {
		zlog.Error().Err(err).Int64("folder", folderID).Msg("record files")
	}
}

// refund gives back what the unwritten files of rq, from index from on
// and costing costs, were paid with.
func (c *controller) refund(ctx context.Context, rq *entity.PutFilesRequest, from int, costs []int) error {
	if rq.Anonymous {
		c.anonymous.Give(fileQuotaKey(rq.UserID), len(costs))
		c.anonBytes.Give(bytesQuotaKey(rq.UserID), uploadSize(rq.Files[from:]))
		return nil
	}
	sum := 0
	for _, n := range costs {
		sum += n
	}
	return c.users.RefundCredits(ctx, rq.UserID, sum, rq.Agent)
}

func (c *controller) maxTTLMinutes(anonymous bool) int {
	if anonymous {
		return c.limits().AnonymousMaxTTLMinutes
	}
	return c.limits().MaxTTLMinutes
}

func (c *controller) maxUploadMB(anonymous bool) int {
	if anonymous {
		return AnonymousMaxUploadMB
	}
	return c.limits().MaxUploadMB
}

func (c *controller) DeleteFile(ctx context.Context, rq *entity.DeleteFileRequest) error {
	if !ValidPath(rq.Path) {
		return ErrInvalidPath
	}
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return err
	}
	if err := c.storage.Delete(ctx, folderPrefix(f)+rq.Path); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	if err := c.folders.DeleteFile(ctx, f.ID, rq.Path); err != nil {
		return fmt.Errorf("delete file record: %w", err)
	}
	return nil
}

func (c *controller) OpenFile(ctx context.Context, rq *entity.OpenFileRequest) (*entity.OpenFileResponse, error) {
	if !ValidPath(rq.Path) {
		return nil, ErrInvalidPath
	}
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	var sign *storage.SignOptions
	if rq.Redirect {
		// The same headers GetFile sends: never rendered, always saved.
		sign = &storage.SignOptions{
			ContentType:        "application/octet-stream",
			ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(rq.Path)}),
		}
	}
	return c.open(ctx, f, rq.Path, sign)
}

func (c *controller) OpenPublicFile(ctx context.Context, rq *entity.OpenPublicFileRequest) (*entity.OpenFileResponse, error) {
	p := rq.Path
	if p == "" || strings.HasSuffix(p, "/") {
		p += _indexFile
	}
	if !ValidPath(p) {
		return nil, ErrNotFound
	}
	// A folder host names no owner; the path form does.
	f, err := c.liveFolder(ctx, rq.FolderID)
	if err != nil {
		return nil, err
	}
	if rq.UserID != 0 && f.UserID != rq.UserID {
		return nil, ErrNotFound
	}
	guarded, unlock, err := c.guard(ctx, f, rq.PinToken, rq.Pin, rq.Client)
	if err != nil {
		return nil, err
	}
	rs, err := c.openPublic(ctx, f, p, rq)
	if err != nil {
		return nil, err
	}
	rs.Private, rs.Unlock = guarded, unlock
	return rs, nil
}

func (c *controller) UnlockFolder(ctx context.Context, rq *entity.UnlockFolderRequest) (*entity.UnlockFolderResponse, error) {
	f, err := c.liveFolder(ctx, rq.FolderID)
	if err != nil {
		return nil, err
	}
	if rq.UserID != 0 && f.UserID != rq.UserID {
		return nil, ErrNotFound
	}
	if rq.Pin == "" {
		return nil, ErrWrongPin
	}
	_, unlock, err := c.guard(ctx, f, "", rq.Pin, rq.Client)
	if err != nil {
		return nil, err
	}
	if unlock == nil {
		// The folder needs no PIN.
		return &entity.UnlockFolderResponse{}, nil
	}
	return unlock, nil
}

// openPublic opens the file at p of a live folder for a visitor; format
// html renders a Markdown file. A visitor who already holds this version
// gets NotModified, without the file being read when its metadata is
// known.
func (c *controller) openPublic(ctx context.Context, f model.Folder, p string, rq *entity.OpenPublicFileRequest) (*entity.OpenFileResponse, error) {
	render := rq.Format == markdown.FormatHTML && markdown.IsMarkdown(p)
	meta, known := f.Files[p]
	if known {
		if rs := notModified(rq, meta.Size, meta.UpdatedAt, render); rs != nil {
			return rs, nil
		}
	}
	// A Markdown page is rendered here, so it needs the file's bytes.
	var sign *storage.SignOptions
	if !render {
		sign = &storage.SignOptions{ContentType: contentType(p)}
	}
	rs, err := c.open(ctx, f, p, sign)
	if err != nil {
		return nil, err
	}
	// Folders from before the metadata column have no meta: the stored
	// object is the version.
	if !known {
		meta = model.FolderFile{Size: rs.Size, UpdatedAt: rs.ModTime}
		if nm := notModified(rq, meta.Size, meta.UpdatedAt, render); nm != nil {
			_ = rs.Body.Close()
			return nm, nil
		}
	}
	rs.ETag, rs.ModTime = etag(meta.Size, meta.UpdatedAt, render), meta.UpdatedAt
	if !render {
		return rs, nil
	}
	return renderMarkdown(rs, p)
}

// etag names a version of a file by its size and write time; a rendered
// Markdown page is another representation of the same file. It is weak,
// so it still matches once the response is compressed.
func etag(size int64, modTime time.Time, render bool) string {
	tag := `W/"` + strconv.FormatInt(size, 36) + "-" + strconv.FormatInt(modTime.UnixNano(), 36)
	if render {
		tag += "-" + markdown.FormatHTML
	}
	return tag + `"`
}

// notModified answers a conditional request for a version the visitor
// already holds, or returns nil. If-None-Match wins over
// If-Modified-Since (RFC 9110 13.2.2).
func notModified(rq *entity.OpenPublicFileRequest, size int64, modTime time.Time, render bool) *entity.OpenFileResponse {
	tag := etag(size, modTime, render)
	switch {
	case rq.IfNoneMatch != "":
		if !etagMatches(rq.IfNoneMatch, tag) {
			return nil
		}
	// HTTP dates have whole seconds.
	case rq.IfModifiedSince.IsZero() || modTime.Truncate(time.Second).After(rq.IfModifiedSince):
		return nil
	}
	return &entity.OpenFileResponse{NotModified: true, ETag: tag, ModTime: modTime}
}

// etagMatches compares an If-None-Match list with tag the weak way: W/
// prefixes don't count.
func etagMatches(list, tag string) bool {
	tag = strings.TrimPrefix(tag, "W/")
	for _, t := range strings.Split(list, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == tag {
			return true
		}
	}
	return false
}

// renderMarkdown replaces the open Markdown file rs with its HTML page.
func renderMarkdown(rs *entity.OpenFileResponse, p string) (*entity.OpenFileResponse, error) {
	defer rs.Body.Close()
	src, err := io.ReadAll(rs.Body)
	if err != nil {
		return nil, fmt.Errorf("read markdown: %w", err)
	}
	page := markdown.Page(p, src)
	return &entity.OpenFileResponse{
		Body:        io.NopCloser(bytes.NewReader(page)),
		Size:        int64(len(page)),
		ContentType: markdown.ContentType,
		ModTime:     rs.ModTime,
		ETag:        rs.ETag,
	}, nil
}

func (c *controller) DeleteExpiredFolders(ctx context.Context) (*entity.DeleteExpiredFoldersResponse, error) {
	folders, err := c.folders.ListExpiredFolders(ctx, c.now(), _sweepBatch)
	if err != nil {
		return &entity.DeleteExpiredFoldersResponse{}, fmt.Errorf("list expired folders: %w", err)
	}
	deleted := 0
	var errs []error
	// One failing folder must not hold up the others.
	for _, f := range folders {
		if err := c.deleteFolder(ctx, f); err != nil {
			errs = append(errs, fmt.Errorf("folder %d: %w", f.ID, err))
			continue
		}
		deleted++
	}
	return &entity.DeleteExpiredFoldersResponse{Deleted: deleted}, errors.Join(errs...)
}

// ValidPath reports whether p is a clean relative file path: slash
// separated, at most MaxDirDepth directories deep, no empty, "." or ".."
// segments, no backslashes or control characters, within the length
// limits. Storage keys are always u<user>/<folder>/<p>, so a valid p can
// never reach outside its folder.
func ValidPath(p string) bool {
	if p == "" || len(p) > _maxPathLength || !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return false
		}
	}
	segs := strings.Split(p, "/")
	if len(segs) > MaxDirDepth+1 {
		return false
	}
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." || len(seg) > _maxSegmentLength || strings.HasPrefix(seg, _reservedPrefix) {
			return false
		}
	}
	return true
}

// validateUploads checks the paths and sizes of an upload. Signed-in users
// are limited per request, anonymous visitors per file; their total is
// capped by the AnonymousBytes quota.
func (c *controller) validateUploads(files []entity.FileUpload, anonymous bool) error {
	if len(files) == 0 {
		return ErrNoFiles
	}
	limit := int64(c.maxUploadMB(anonymous)) << 20
	var total int64
	seen := make(map[string]bool, len(files))
	for _, u := range files {
		if !ValidPath(u.Path) {
			return fmt.Errorf("%w: %q", ErrInvalidPath, u.Path)
		}
		if seen[u.Path] {
			return fmt.Errorf("%w: %q", ErrDuplicatePath, u.Path)
		}
		seen[u.Path] = true
		if anonymous && int64(len(u.Data)) > limit {
			return fmt.Errorf("%w: %q is over the %d MB file limit", ErrTooLarge, u.Path, limit>>20)
		}
		total += int64(len(u.Data))
	}
	if !anonymous && total > limit {
		return fmt.Errorf("%w: limit is %d MB", ErrTooLarge, limit>>20)
	}
	return nil
}

// liveFolder finds a folder that has not expired yet. Expired folders are
// gone to users even before the sweep removes them.
func (c *controller) liveFolder(ctx context.Context, id int64) (model.Folder, error) {
	f, err := c.folders.FindFolderByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return model.Folder{}, ErrNotFound
	}
	if err != nil {
		return model.Folder{}, fmt.Errorf("find folder: %w", err)
	}
	if !c.now().Before(f.ExpiresAt) {
		return model.Folder{}, ErrNotFound
	}
	return f, nil
}

func (c *controller) ownedFolder(ctx context.Context, userID, id int64) (model.Folder, error) {
	f, err := c.liveFolder(ctx, id)
	if err != nil {
		return model.Folder{}, err
	}
	if f.UserID != userID {
		return model.Folder{}, ErrNotFound
	}
	return f, nil
}

// ownedFolderAndOwner finds one of a user's live folders, and the user,
// whose account PIN may guard it.
func (c *controller) ownedFolderAndOwner(ctx context.Context, userID, id int64) (model.Folder, model.User, error) {
	f, err := c.ownedFolder(ctx, userID, id)
	if err != nil {
		return model.Folder{}, model.User{}, err
	}
	owner, err := c.owner(ctx, userID)
	if err != nil {
		return model.Folder{}, model.User{}, err
	}
	return f, owner, nil
}

// owner finds the user who owns folders, for their PIN settings. An
// anonymous visitor has no account, so no settings.
func (c *controller) owner(ctx context.Context, userID int64) (model.User, error) {
	u, err := c.users.FindUserByID(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return model.User{ID: userID}, nil
	}
	if err != nil {
		return model.User{}, fmt.Errorf("find owner: %w", err)
	}
	return u, nil
}

// lock is what guards a folder: the mode of its PIN, a fingerprint of the
// PIN as it is stored, and the check of a PIN.
type lock struct {
	mode        string
	fingerprint string
	match       func(p string) bool
}

// lockOf returns what guards f, owned by owner: their account PIN, else
// the folder's own; nil when nothing does.
func (c *controller) lockOf(f model.Folder, owner model.User) *lock {
	switch {
	case owner.PinMode == model.PinModeTOTP:
		return &lock{mode: entity.PinModeTOTP, fingerprint: pin.Fingerprint(entity.PinModeTOTP, owner.TOTPSecret), match: func(p string) bool {
			secret, err := pin.OpenSecret(c.secrets, owner.ID, owner.TOTPSecret)
			if err != nil {
				zlog.Error().Err(err).Int64("user", owner.ID).Msg("open totp secret")
				return false
			}
			return pin.ValidTOTP(secret, p, c.now())
		}}
	case owner.PinMode == model.PinModeStatic:
		return &lock{mode: entity.PinModeStatic, fingerprint: pin.Fingerprint(entity.PinModeStatic, owner.PinHash), match: func(p string) bool {
			return pin.Match(owner.PinHash, p)
		}}
	case f.PinHash != "":
		return &lock{mode: entity.PinModeFolder, fingerprint: pin.Fingerprint(entity.PinModeFolder, f.PinHash), match: func(p string) bool {
			return pin.Match(f.PinHash, p)
		}}
	}
	return nil
}

// guard lets a visitor of f through when nothing guards it, when their
// token proves they entered its current PIN, or when p is that PIN; then
// it returns the token to keep. guarded reports whether a PIN guards f.
// Each visitor may enter PinAttempts wrong PINs per PinWindow.
func (c *controller) guard(ctx context.Context, f model.Folder, token, p, client string) (guarded bool, unlock *entity.UnlockFolderResponse, err error) {
	owner, err := c.owner(ctx, f.UserID)
	if err != nil {
		return false, nil, err
	}
	l := c.lockOf(f, owner)
	if l == nil {
		return false, nil, nil
	}
	if id, fp, err := c.tokens.ValidateFolderPinToken(token); err == nil && id == idgen.FormatFolderID(f.ID) && fp == l.fingerprint {
		return true, nil, nil
	}
	if p == "" {
		return true, nil, ErrPinRequired
	}
	key := "pin:" + idgen.FormatFolderID(f.ID) + ":" + client
	if !c.pins.Take(key, 1) {
		return true, nil, fmt.Errorf("%w: at most %d per %d seconds", ErrPinRateLimited, PinAttempts, int(PinWindow/time.Second))
	}
	if !l.match(p) {
		return true, nil, ErrWrongPin
	}
	// Only wrong PINs count.
	c.pins.Give(key, 1)
	signed, err := c.tokens.CreateFolderPinToken(idgen.FormatFolderID(f.ID), l.fingerprint, f.ExpiresAt)
	if err != nil {
		return true, nil, fmt.Errorf("sign pin token: %w", err)
	}
	return true, &entity.UnlockFolderResponse{Token: signed, ExpiresAt: f.ExpiresAt.UTC()}, nil
}

// open opens a file. With sign, a file over the redirect size comes as a
// signed URL that answers with sign's headers, when the storage signs.
// The size comes from the folder's Files, so deciding takes no request to
// the storage. An HTML file is never redirected, whatever its size: a page
// stays on our domain, where its relative links lead to the folder's other
// files and it renders sandboxed.
func (c *controller) open(ctx context.Context, f model.Folder, p string, sign *storage.SignOptions) (*entity.OpenFileResponse, error) {
	key := folderPrefix(f) + p
	meta, known := f.Files[p]
	if sign != nil && isHTML(p) {
		sign = nil
	}
	if signer, ok := c.storage.(storage.Signer); ok && known && sign != nil && c.redirect > 0 && meta.Size > c.redirect {
		opts := *sign
		opts.TTL = c.ttl
		u, err := signer.SignGet(ctx, key, opts)
		if err != nil {
			return nil, fmt.Errorf("sign file: %w", err)
		}
		if u != "" {
			return &entity.OpenFileResponse{RedirectURL: u, Size: meta.Size, ContentType: contentType(p), ModTime: meta.UpdatedAt}, nil
		}
	}
	body, obj, err := c.storage.Get(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	return &entity.OpenFileResponse{Body: body, Size: obj.Size, ContentType: contentType(p), ModTime: obj.ModTime}, nil
}

// objects lists a folder's files: from its Files, or from the storage for a
// folder created before Files was kept.
func (c *controller) objects(ctx context.Context, f model.Folder) ([]storage.Object, error) {
	if f.Files == nil {
		return c.storage.List(ctx, folderPrefix(f))
	}
	out := make([]storage.Object, 0, len(f.Files))
	for p, m := range f.Files {
		out = append(out, storage.Object{Key: folderPrefix(f) + p, Size: m.Size, ModTime: m.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// isHTML reports whether the file at p is served as an HTML page.
func isHTML(p string) bool {
	return strings.HasPrefix(contentType(p), "text/html")
}

// contentType is the media type a file is served with, by its extension.
func contentType(p string) string {
	if t := mime.TypeByExtension(path.Ext(p)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func (c *controller) deleteFolder(ctx context.Context, f model.Folder) error {
	if err := c.storage.DeletePrefix(ctx, folderPrefix(f)); err != nil {
		return fmt.Errorf("delete files: %w", err)
	}
	if err := c.folders.DeleteFolder(ctx, f.ID); err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}
	return nil
}

func (c *controller) folderEntity(f model.Folder, owner model.User) entity.Folder {
	e := mappermodel.FromFolderModelToFolderEntity(f)
	if l := c.lockOf(f, owner); l != nil {
		e.PinMode = l.mode
	}
	e.CreatedAt, e.ExpiresAt = e.CreatedAt.UTC(), e.ExpiresAt.UTC()
	e.URL = c.fileURL(f, "")
	e.DefaultTitle = idgen.FormatFolderID(f.ID)
	return e
}

// fileURL is the public URL of a file, or of the folder for an empty path:
// on the folder's own host with subdomains on, else below the app's URL.
func (c *controller) fileURL(f model.Folder, p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	rel := strings.Join(segs, "/")
	if s := c.limits(); s.Subdomains {
		return c.scheme + "://" + FolderHost(f.ID) + "." + s.PublicDomain + "/" + rel
	}
	return c.baseURL + PublicPathPrefix + idgen.FormatBase36(f.UserID) + "/" + idgen.FormatFolderID(f.ID) + "/" + rel
}

// FolderHost is the leftmost hostname label a folder is served under: its
// ID.
func FolderHost(folderID int64) string {
	return idgen.FormatFolderID(folderID)
}

// folderPrefix is where a folder's files live in storage.
func folderPrefix(f model.Folder) string {
	return "u" + idgen.FormatBase36(f.UserID) + "/" + idgen.FormatFolderID(f.ID) + "/"
}

// fileQuotaKey and folderQuotaKey are an anonymous user's quota keys.
func fileQuotaKey(userID int64) string { return "files:" + idgen.FormatBase36(userID) }

func folderQuotaKey(userID int64) string { return "folders:" + idgen.FormatBase36(userID) }

func bytesQuotaKey(userID int64) string { return "bytes:" + idgen.FormatBase36(userID) }

// uploadSize is the total size of files.
func uploadSize(files []entity.FileUpload) int {
	n := 0
	for _, u := range files {
		n += len(u.Data)
	}
	return n
}
