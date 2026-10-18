package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/model"
)

func TestSettingsRepository_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	repo := NewSettingsRepository(path)

	_, err := repo.LoadSettings()
	assert.ErrorIs(t, err, ErrNotFound)

	want := model.Settings{Credits: model.CreditSettings{MonthlyGrant: 99}, Auth: model.AuthSettings{Google: model.OAuthClientSettings{ClientSecret: "s"}}}
	require.NoError(t, repo.SaveSettings(want))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "holds secrets")
	_, err = os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err))

	got, err := repo.LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSettingsRepository_Errors(t *testing.T) {
	dir := t.TempDir()

	bad := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{nope"), 0o600))
	_, err := NewSettingsRepository(bad).LoadSettings()
	assert.ErrorContains(t, err, "parse settings")

	// Reading a directory fails with something other than not-exist.
	_, err = NewSettingsRepository(dir).LoadSettings()
	assert.ErrorContains(t, err, "read settings")

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	assert.ErrorContains(t, NewSettingsRepository(filepath.Join(file, "x", "s.json")).SaveSettings(model.Settings{}), "create settings dir")

	readonly := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(readonly, 0o500))
	assert.ErrorContains(t, NewSettingsRepository(filepath.Join(readonly, "s.json")).SaveSettings(model.Settings{}), "write settings")

	target := filepath.Join(dir, "target")
	require.NoError(t, os.MkdirAll(filepath.Join(target, "child"), 0o700))
	assert.ErrorContains(t, NewSettingsRepository(target).SaveSettings(model.Settings{}), "replace settings")
}
