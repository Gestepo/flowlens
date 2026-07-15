//go:build linux

package ebpf

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeRecordPreservesIPv4AndNetworkByteOrder(t *testing.T) {
	record := make([]byte, rawEventSize)
	binary.LittleEndian.PutUint64(record[0:8], 123456789)
	binary.LittleEndian.PutUint64(record[8:16], 9876)
	binary.LittleEndian.PutUint32(record[16:20], 4242)
	record[20] = 2
	record[21] = 6
	record[22] = 1
	binary.BigEndian.PutUint16(record[24:26], 49152)
	binary.BigEndian.PutUint16(record[26:28], 443)
	copy(record[28:32], []byte{10, 0, 0, 2})
	copy(record[44:48], []byte{203, 0, 113, 10})
	binary.LittleEndian.PutUint64(record[64:72], 1024)
	binary.LittleEndian.PutUint64(record[72:80], 2048)

	observation, err := decodeRecord(record)
	require.NoError(t, err)
	require.Equal(t, uint64(123456789), observation.MonotonicNS)
	require.Equal(t, uint64(9876), observation.CgroupID)
	require.Equal(t, uint32(4242), observation.PID)
	require.Equal(t, uint8(6), observation.Protocol)
	require.Equal(t, "10.0.0.2", observation.LocalIP.String())
	require.Equal(t, "203.0.113.10", observation.RemoteIP.String())
	require.Equal(t, uint16(49152), observation.LocalPort)
	require.Equal(t, uint16(443), observation.RemotePort)
	require.Equal(t, uint64(1024), observation.Sent)
	require.Equal(t, uint64(2048), observation.Received)
	require.Equal(t, uint8(1), observation.State)
}

func TestDecodeRecordRejectsShortRecords(t *testing.T) {
	_, err := decodeRecord(make([]byte, rawEventSize-1))
	require.ErrorContains(t, err, "record size")
}
