package docker

import (
	"context"
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

const testContainerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestContainerIDFromCgroupPathSupportsDockerAndSystemdScopes(t *testing.T) {
	for _, path := range []string{
		"/docker/" + testContainerID,
		"/system.slice/docker-" + testContainerID + ".scope",
	} {
		id, ok := ContainerIDFromCgroupPath(path)
		require.True(t, ok)
		require.Equal(t, testContainerID, id)
	}
	_, ok := ContainerIDFromCgroupPath("/system.slice/not-a-container.scope")
	require.False(t, ok)
}

func TestRefreshRetainsRunningContainerAddressesAndPorts(t *testing.T) {
	api := &fakeAPI{
		list: client.ContainerListResult{Items: []container.Summary{
			{ID: testContainerID, State: container.StateRunning},
			{ID: "stopped", State: container.StateExited},
		}},
		inspect: map[string]client.ContainerInspectResult{
			testContainerID: {Container: container.InspectResponse{
				ID:     testContainerID,
				Name:   "/web",
				State:  &container.State{Pid: 123, Running: true},
				Config: &container.Config{ExposedPorts: network.PortSet{network.MustParsePort("8080/tcp"): struct{}{}, network.MustParsePort("8443/tcp"): struct{}{}}},
				NetworkSettings: &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{
					"bridge": {IPAddress: netip.MustParseAddr("172.18.0.2"), GlobalIPv6Address: netip.MustParseAddr("2001:db8::2")},
				}},
			}},
		},
	}
	inventory := NewInventory(api, func(pid int) (Cgroup, error) {
		require.Equal(t, 123, pid)
		return Cgroup{ID: 77, Path: "/system.slice/docker-" + testContainerID + ".scope"}, nil
	})

	snapshot, err := inventory.Refresh(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.ByID, 1)
	owner := snapshot.ByID[testContainerID]
	require.Equal(t, "web", owner.Name)
	require.Equal(t, uint64(77), owner.CgroupID)
	require.ElementsMatch(t, []string{"172.18.0.2", "2001:db8::2"}, owner.Addresses)
	require.ElementsMatch(t, []uint16{8080, 8443}, owner.Ports)
	require.Equal(t, owner, snapshot.ByCgroup[77])
	require.Equal(t, owner, snapshot.ByEndpoint["172.18.0.2:8080"])

	api.list = client.ContainerListResult{}
	snapshot, err = inventory.Refresh(context.Background())
	require.NoError(t, err)
	require.Empty(t, snapshot.ByID)
}

type fakeAPI struct {
	list    client.ContainerListResult
	inspect map[string]client.ContainerInspectResult
}

func (api *fakeAPI) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	return api.list, nil
}

func (api *fakeAPI) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return api.inspect[id], nil
}
