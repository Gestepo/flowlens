//go:build !linux

package ebpf

import "context"

type UnsupportedTracer struct{}

func NewTracer() (Tracer, error) { return nil, ErrUnsupportedPlatform }

func (UnsupportedTracer) Run(context.Context, chan<- Observation) error {
	return ErrUnsupportedPlatform
}

func (UnsupportedTracer) Close() error { return nil }
