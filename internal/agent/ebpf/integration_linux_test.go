//go:build linux

package ebpf

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTracerObservesExactTCPTransfer(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires CAP_BPF or CAP_SYS_ADMIN")
	}

	tracer, err := NewTracer()
	if err != nil && (errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission")) {
		t.Skipf("requires CAP_BPF or CAP_SYS_ADMIN: %v", err)
	}
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tracer.Close()) })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	observations := make(chan Observation, 4096)
	runErrors := make(chan error, 1)
	go func() { runErrors <- tracer.Run(ctx, observations) }()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	serverDone := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer connection.Close()
		_, copyErr := io.Copy(io.Discard, io.LimitReader(connection, 1<<20))
		serverDone <- copyErr
	}()

	connection, err := net.Dial("tcp4", listener.Addr().String())
	require.NoError(t, err)
	_, err = io.CopyN(connection, zeroReader{}, 1<<20)
	require.NoError(t, err)
	require.NoError(t, connection.Close())
	require.NoError(t, <-serverDone)

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var sent, received uint64
	for sent < 1<<20 || received < 1<<20 {
		select {
		case observation := <-observations:
			if observation.LocalPort == port || observation.RemotePort == port {
				sent += observation.Sent
				received += observation.Received
			}
		case err := <-runErrors:
			require.NoError(t, err)
		case <-deadline.C:
			require.FailNow(t, "timed out waiting for transfer observations", "sent=%d received=%d", sent, received)
		}
	}
	require.InDelta(t, 1<<20, sent, float64(1<<20)*0.02)
	require.InDelta(t, 1<<20, received, float64(1<<20)*0.02)
	cancel()
	require.NoError(t, tracer.Close())
	select {
	case err := <-runErrors:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "tracer Run did not stop")
	}
	require.NoError(t, tracer.Close())
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}
