//go:build linux

package ebpf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

type socketTracer struct {
	objects   socketFlowObjects
	links     []link.Link
	reader    *ringbuf.Reader
	closeOnce sync.Once
	closeErr  error
}

func NewTracer() (Tracer, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("remove memlock limit: %w", err)
	}

	tracer := &socketTracer{}
	if err := loadSocketFlowObjects(&tracer.objects, nil); err != nil {
		return nil, fmt.Errorf("load socket flow objects: %w", err)
	}
	attachments := []struct {
		symbol string
		attach func(string, *socketFlowPrograms) (link.Link, error)
	}{
		{symbol: "tcp_sendmsg", attach: func(symbol string, programs *socketFlowPrograms) (link.Link, error) {
			return link.Kprobe(symbol, programs.TraceTcpSendmsg, nil)
		}},
		{symbol: "tcp_sendmsg", attach: func(symbol string, programs *socketFlowPrograms) (link.Link, error) {
			return link.Kretprobe(symbol, programs.TraceTcpSendmsgReturn, nil)
		}},
		{symbol: "tcp_cleanup_rbuf", attach: func(symbol string, programs *socketFlowPrograms) (link.Link, error) {
			return link.Kprobe(symbol, programs.TraceTcpCleanupRbuf, nil)
		}},
		{symbol: "udp_sendmsg", attach: func(symbol string, programs *socketFlowPrograms) (link.Link, error) {
			return link.Kprobe(symbol, programs.TraceUdpSendmsg, nil)
		}},
		{symbol: "udp_sendmsg", attach: func(symbol string, programs *socketFlowPrograms) (link.Link, error) {
			return link.Kretprobe(symbol, programs.TraceUdpSendmsgReturn, nil)
		}},
		{symbol: "udp_recvmsg", attach: func(symbol string, programs *socketFlowPrograms) (link.Link, error) {
			return link.Kprobe(symbol, programs.TraceUdpRecvmsg, nil)
		}},
		{symbol: "udp_recvmsg", attach: func(symbol string, programs *socketFlowPrograms) (link.Link, error) {
			return link.Kretprobe(symbol, programs.TraceUdpRecvmsgReturn, nil)
		}},
	}
	for _, attachment := range attachments {
		attached, err := attachment.attach(attachment.symbol, &tracer.objects.socketFlowPrograms)
		if err != nil {
			_ = tracer.Close()
			return nil, fmt.Errorf("attach %s: %w", attachment.symbol, err)
		}
		tracer.links = append(tracer.links, attached)
	}

	reader, err := ringbuf.NewReader(tracer.objects.Events)
	if err != nil {
		_ = tracer.Close()
		return nil, fmt.Errorf("open socket flow ring buffer: %w", err)
	}
	tracer.reader = reader
	return tracer, nil
}

func (tracer *socketTracer) Run(ctx context.Context, output chan<- Observation) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		tracer.reader.SetDeadline(time.Now().Add(time.Second))
		record, err := tracer.reader.Read()
		if err != nil {
			switch {
			case errors.Is(err, os.ErrDeadlineExceeded):
				continue
			case errors.Is(err, ringbuf.ErrClosed), ctx.Err() != nil:
				return nil
			default:
				return fmt.Errorf("read socket flow event: %w", err)
			}
		}
		observation, err := decodeRecord(record.RawSample)
		if err != nil {
			return err
		}
		select {
		case output <- observation:
		case <-ctx.Done():
			return nil
		}
	}
}

func (tracer *socketTracer) Close() error {
	tracer.closeOnce.Do(func() {
		var closeErrors []error
		if tracer.reader != nil {
			closeErrors = append(closeErrors, tracer.reader.Close())
		}
		for index := len(tracer.links) - 1; index >= 0; index-- {
			closeErrors = append(closeErrors, tracer.links[index].Close())
		}
		closeErrors = append(closeErrors, tracer.objects.Close())
		tracer.closeErr = errors.Join(closeErrors...)
	})
	return tracer.closeErr
}
