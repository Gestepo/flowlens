package process

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLookupReturnsSanitizedNameAndCgroupWithoutCommandLine(t *testing.T) {
	root := t.TempDir()
	writeProcessFixture(t, root, "42", "curl\n", "curl --header Authorization:secret-token\x00", "0::/system.slice/demo.scope\n", "12345")
	lookup := NewLookup(root, time.Now)

	result, ok := lookup.Lookup(42)
	require.True(t, ok)
	require.Equal(t, uint32(42), result.PID)
	require.Equal(t, "curl", result.Name)
	require.Equal(t, "/system.slice/demo.scope", result.CgroupPath)
	require.NotContains(t, result.Name, "secret-token")
}

func TestLookupReturnsUnattributedWhenProcessExits(t *testing.T) {
	lookup := NewLookup(t.TempDir(), time.Now)
	_, ok := lookup.Lookup(99999)
	require.False(t, ok)
}

func TestLookupCacheRejectsPIDReuseByStartTime(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	lookup := NewLookup(root, func() time.Time { return now })
	writeProcessFixture(t, root, "42", "first\n", "", "0::/first\n", "100")
	first, ok := lookup.Lookup(42)
	require.True(t, ok)
	require.Equal(t, "first", first.Name)

	writeProcessFixture(t, root, "42", "second\n", "", "0::/second\n", "200")
	second, ok := lookup.Lookup(42)
	require.True(t, ok)
	require.Equal(t, "second", second.Name)
	require.Equal(t, uint64(200), second.StartTime)
}

func writeProcessFixture(t *testing.T, root, pid, comm, cmdline, cgroup, startTime string) {
	t.Helper()
	directory := filepath.Join(root, pid)
	require.NoError(t, os.MkdirAll(directory, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "comm"), []byte(comm), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "cmdline"), []byte(cmdline), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "cgroup"), []byte(cgroup), 0o600))
	fields := "S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 " + startTime + " 20"
	require.NoError(t, os.WriteFile(filepath.Join(directory, "stat"), []byte(pid+" (fixture name) "+fields+"\n"), 0o600))
}
