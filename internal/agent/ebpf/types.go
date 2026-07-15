package ebpf

import (
	"context"
	"errors"
	"net/netip"
)

var ErrUnsupportedPlatform = errors.New("eBPF socket tracing is unsupported on this platform")

type Observation struct {
	MonotonicNS uint64
	CgroupID    uint64
	PID         uint32
	Protocol    uint8
	LocalIP     netip.Addr
	RemoteIP    netip.Addr
	LocalPort   uint16
	RemotePort  uint16
	Sent        uint64
	Received    uint64
	State       uint8
}

type Tracer interface {
	Run(context.Context, chan<- Observation) error
	Close() error
}
