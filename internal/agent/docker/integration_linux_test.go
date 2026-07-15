//go:build linux

package docker

import (
	"context"
	"os"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestLiveInventoryFindsNamedContainer(t *testing.T) {
	wanted := os.Getenv("FLOWLENS_TEST_CONTAINER")
	if wanted == "" {
		t.Skip("FLOWLENS_TEST_CONTAINER is not configured")
	}
	api, err := client.New(client.FromEnv)
	require.NoError(t, err)
	t.Cleanup(func() { _ = api.Close() })
	inventory := NewInventory(api, func(pid int) (Cgroup, error) {
		return ResolveHostCgroup("/proc", "/sys/fs/cgroup", pid)
	})
	snapshot, err := inventory.Refresh(context.Background())
	require.NoError(t, err)
	for _, container := range snapshot.ByID {
		if container.Name == wanted {
			require.NotZero(t, container.CgroupID)
			return
		}
	}
	require.Fail(t, "named container missing from inventory", "name=%s", wanted)
}
