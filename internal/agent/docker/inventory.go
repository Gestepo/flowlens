package docker

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

var containerCgroupPattern = regexp.MustCompile(`(?:docker/|docker-)([0-9a-f]{64})(?:\.scope)?(?:$|/)`)

type API interface {
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
}

type Cgroup struct {
	ID   uint64
	Path string
}

type CgroupResolver func(pid int) (Cgroup, error)

type Container struct {
	ID        string
	Name      string
	CgroupID  uint64
	Addresses []string
	Ports     []uint16
	Running   bool
}

type Snapshot struct {
	ByCgroup   map[uint64]Container
	ByID       map[string]Container
	ByEndpoint map[string]Container
}

type Inventory struct {
	api     API
	resolve CgroupResolver
}

func NewInventory(api API, resolve CgroupResolver) *Inventory {
	return &Inventory{api: api, resolve: resolve}
}

func (inventory *Inventory) Refresh(ctx context.Context) (Snapshot, error) {
	listed, err := inventory.api.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return Snapshot{}, fmt.Errorf("list Docker containers: %w", err)
	}
	snapshot := Snapshot{
		ByCgroup:   make(map[uint64]Container),
		ByID:       make(map[string]Container),
		ByEndpoint: make(map[string]Container),
	}
	for _, summary := range listed.Items {
		if summary.State != container.StateRunning {
			continue
		}
		inspected, err := inventory.api.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
		if err != nil {
			return Snapshot{}, fmt.Errorf("inspect Docker container %s: %w", summary.ID, err)
		}
		value := inspected.Container
		if value.State == nil || !value.State.Running || value.State.Pid <= 0 {
			continue
		}
		cgroup, err := inventory.resolve(value.State.Pid)
		if err != nil {
			return Snapshot{}, fmt.Errorf("resolve Docker container %s cgroup: %w", summary.ID, err)
		}
		if cgroupContainerID, ok := ContainerIDFromCgroupPath(cgroup.Path); ok && cgroupContainerID != value.ID {
			return Snapshot{}, fmt.Errorf("container %s cgroup belongs to %s", value.ID, cgroupContainerID)
		}
		owner := Container{
			ID:        value.ID,
			Name:      strings.TrimPrefix(value.Name, "/"),
			CgroupID:  cgroup.ID,
			Addresses: containerAddresses(value),
			Ports:     containerPorts(value),
			Running:   true,
		}
		snapshot.ByID[owner.ID] = owner
		snapshot.ByCgroup[owner.CgroupID] = owner
		for _, address := range owner.Addresses {
			for _, port := range owner.Ports {
				snapshot.ByEndpoint[net.JoinHostPort(address, strconv.Itoa(int(port)))] = owner
			}
		}
	}
	return snapshot, nil
}

func ContainerIDFromCgroupPath(path string) (string, bool) {
	match := containerCgroupPattern.FindStringSubmatch(path)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func ResolveHostCgroup(procRoot, cgroupRoot string, pid int) (Cgroup, error) {
	contents, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return Cgroup{}, err
	}
	var path string
	for _, line := range strings.Split(string(contents), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[2] != "" {
			path = parts[2]
			break
		}
	}
	if path == "" {
		return Cgroup{}, fmt.Errorf("process %d has no cgroup path", pid)
	}
	info, err := os.Stat(filepath.Join(cgroupRoot, strings.TrimPrefix(path, "/")))
	if err != nil {
		return Cgroup{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Cgroup{}, fmt.Errorf("cgroup stat has unexpected type")
	}
	return Cgroup{ID: stat.Ino, Path: path}, nil
}

func containerAddresses(value container.InspectResponse) []string {
	var addresses []string
	if value.NetworkSettings != nil {
		for _, endpoint := range value.NetworkSettings.Networks {
			if endpoint == nil {
				continue
			}
			if endpoint.IPAddress.IsValid() {
				addresses = append(addresses, endpoint.IPAddress.String())
			}
			if endpoint.GlobalIPv6Address.IsValid() {
				addresses = append(addresses, endpoint.GlobalIPv6Address.String())
			}
		}
	}
	sort.Strings(addresses)
	return addresses
}

func containerPorts(value container.InspectResponse) []uint16 {
	var ports []uint16
	if value.Config != nil {
		for port := range value.Config.ExposedPorts {
			ports = append(ports, port.Num())
		}
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	return ports
}
