package repository

//go:generate mockgen -source=settings.go -destination=mocks/settings.go -package=mocks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tmphost/tmphost/backend/internal/data/model"
)

type (
	// SettingsRepository persists the setup page settings. They live in a
	// file, not the database, because the database choice is a setting.
	SettingsRepository interface {
		// LoadSettings returns the saved settings, or ErrNotFound when none
		// were saved yet.
		LoadSettings() (model.Settings, error)
		// SaveSettings atomically replaces the saved settings.
		SaveSettings(s model.Settings) error
	}

	settingsRepository struct {
		path string
	}
)

// NewSettingsRepository stores settings as JSON at path.
func NewSettingsRepository(path string) SettingsRepository {
	return &settingsRepository{path: path}
}

func (r *settingsRepository) LoadSettings() (model.Settings, error) {
	b, err := os.ReadFile(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return model.Settings{}, ErrNotFound
	}
	if err != nil {
		return model.Settings{}, fmt.Errorf("read settings: %w", err)
	}
	var s model.Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return model.Settings{}, fmt.Errorf("parse settings %s: %w", r.path, err)
	}
	return s, nil
}

func (r *settingsRepository) SaveSettings(s model.Settings) error {
	// A struct of plain fields always encodes.
	b, _ := json.MarshalIndent(s, "", "  ")
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	// The file holds secrets: owner-only, and written via rename so a crash
	// never leaves it half written.
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	defer os.Remove(tmp)
	if err := os.Rename(tmp, r.path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}
