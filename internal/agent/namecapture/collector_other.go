//go:build !linux

package namecapture

import (
	"context"
	"errors"

	"flowlens/internal/model"
)

var ErrUnsupportedPlatform = errors.New("AF_PACKET name capture is unsupported on this platform")

type Collector struct{}

func NewCollector([]string) (*Collector, error) { return nil, ErrUnsupportedPlatform }

func (*Collector) Run(context.Context, chan<- model.NameEvidence) error {
	return ErrUnsupportedPlatform
}

func (*Collector) Close() error { return nil }

func (*Collector) Stats() ProcessorStats { return ProcessorStats{} }
