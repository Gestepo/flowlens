package npm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTailerFinishesRotatedInodeAndResumesCursor(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "proxy-host-1_access.log")
	lines := fixtureLines(t)
	require.NoError(t, os.WriteFile(path, []byte(lines[0]+"\n"), 0o600))
	tailer := NewTailer(filepath.Join(directory, "cursors.json"), []string{directory}, false)

	result, err := tailer.Poll([]string{path})
	require.NoError(t, err)
	require.Len(t, result.Requests, 1)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = file.WriteString(lines[1] + "\n")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.NoError(t, os.Rename(path, path+".1"))
	require.NoError(t, os.WriteFile(path, []byte(lines[2]+"\n"), 0o600))

	result, err = tailer.Poll([]string{path})
	require.NoError(t, err)
	require.Len(t, result.Requests, 2)
	require.Equal(t, "api.example.test", result.Requests[0].Request.Host)
	require.Equal(t, "ipv6.example.test", result.Requests[1].Request.Host)

	restarted := NewTailer(filepath.Join(directory, "cursors.json"), []string{directory}, false)
	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = file.WriteString(lines[3] + "\n")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	result, err = restarted.Poll([]string{path})
	require.NoError(t, err)
	require.Len(t, result.Requests, 1)
}

func TestTailerResetsTruncatedFileAndCountsMalformedLines(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "proxy-host-1_access.log")
	lines := fixtureLines(t)
	require.NoError(t, os.WriteFile(path, []byte(lines[0]+"\n"), 0o600))
	tailer := NewTailer(filepath.Join(directory, "cursors.json"), []string{directory}, false)
	_, err := tailer.Poll([]string{path})
	require.NoError(t, err)

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	require.NoError(t, err)
	_, err = file.WriteString("bad line\n" + lines[2] + "\n")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	result, err := tailer.Poll([]string{path})
	require.NoError(t, err)
	require.True(t, result.Truncated)
	require.Equal(t, 1, result.Malformed)
	require.Len(t, result.Requests, 1)
}

func TestTailerRejectsGlobsOutsideAllowedRoots(t *testing.T) {
	directory := t.TempDir()
	tailer := NewTailer(filepath.Join(directory, "cursors.json"), []string{filepath.Join(directory, "allowed")}, false)
	_, err := tailer.Poll([]string{filepath.Join(directory, "outside", "*.log")})
	require.ErrorContains(t, err, "allowed roots")
}

func fixtureLines(t *testing.T) []string {
	t.Helper()
	contents, err := os.ReadFile("testdata/access.log")
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(contents)), "\n")
}
