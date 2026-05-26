//go:build linux

package openaisub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestCheckStatusRestrictsPermissiveAuthFileBeforeRefreshWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "auth")
	path := filepath.Join(dir, "openai_auth.json")
	dirMode := os.FileMode(0o755)
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	require.NoError(t, os.MkdirAll(dir, dirMode))
	writeAuthFileWithMode(t, path, authFile{
		Type:             authType,
		AccessToken:      "old-access-token",
		RefreshToken:     "old-refresh-token",
		IDToken:          jwtForAccount(t, "old-account-id"),
		ExpiresAt:        now.Add(-time.Hour),
		ChatGPTAccountID: "old-account-id",
	}, 0o644)
	require.NoError(t, os.Chmod(dir, dirMode))
	require.NoError(t, os.Chmod(path, 0o644))

	inotifyFD := watchInotify(t, path, unix.IN_ATTRIB|unix.IN_MODIFY|unix.IN_CLOSE_WRITE)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, oauthTokenPath, r.URL.Path)
		require.NoError(t, r.ParseForm())
		_, _ = w.Write([]byte(`{
			"access_token":"new-access-token",
			"refresh_token":"new-refresh-token",
			"id_token":"` + jwtForAccount(t, "new-account-id") + `",
			"expires_in":3600
		}`))
	}))
	defer server.Close()

	status, err := CheckStatusWithOptions(context.Background(), Options{
		Path:        path,
		Now:         func() time.Time { return now },
		OAuthIssuer: server.URL,
	})
	require.NoError(t, err)
	assert.True(t, status.LoggedIn)

	masks := readInotifyEventMasks(t, inotifyFD)
	firstAttrib := firstInotifyMask(masks, unix.IN_ATTRIB)
	firstWrite := firstInotifyMask(masks, unix.IN_MODIFY|unix.IN_CLOSE_WRITE)
	require.NotEqual(t, -1, firstAttrib)
	require.NotEqual(t, -1, firstWrite)
	assert.Less(t, firstAttrib, firstWrite)

	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, dirMode, dirInfo.Mode().Perm())
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "new-access-token")
}

func writeAuthFileWithMode(t *testing.T, path string, auth authFile, mode os.FileMode) {
	t.Helper()
	data, err := json.Marshal(auth)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, mode))
}

func watchInotify(t *testing.T, path string, mask uint32) int {
	t.Helper()
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = unix.Close(fd)
	})
	_, err = unix.InotifyAddWatch(fd, path, mask)
	require.NoError(t, err)
	return fd
}

func readInotifyEventMasks(t *testing.T, fd int) []uint32 {
	t.Helper()
	buf := make([]byte, 4096)
	n, err := unix.Read(fd, buf)
	if err == unix.EAGAIN {
		return nil
	}
	require.NoError(t, err)

	var masks []uint32
	for offset := 0; offset+unix.SizeofInotifyEvent <= n; {
		event := (*unix.InotifyEvent)(unsafe.Pointer(&buf[offset]))
		masks = append(masks, event.Mask)
		offset += unix.SizeofInotifyEvent + int(event.Len)
	}
	return masks
}

func firstInotifyMask(masks []uint32, mask uint32) int {
	for i, eventMask := range masks {
		if eventMask&mask != 0 {
			return i
		}
	}
	return -1
}
