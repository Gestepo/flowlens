package namecapture

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractSNIFromTLS12AndFragmentedTLS13ClientHello(t *testing.T) {
	handshake := clientHello(t, "Api.Example.COM", false)
	name, ok, err := ExtractSNI(tlsRecord(handshake))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "api.example.com", name)

	split := len(handshake) / 2
	fragmented := append(tlsRecord(handshake[:split]), tlsRecord(handshake[split:])...)
	name, ok, err = ExtractSNI(fragmented)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "api.example.com", name)
}

func TestExtractSNIRejectsECHOnlyMissingMalformedAndOversizedPayloads(t *testing.T) {
	for _, payload := range [][]byte{
		tlsRecord(clientHello(t, "", false)),
		tlsRecord(clientHello(t, "", true)),
	} {
		name, ok, err := ExtractSNI(payload)
		require.NoError(t, err)
		require.False(t, ok)
		require.Empty(t, name)
	}

	_, _, err := ExtractSNI([]byte{22, 3, 3, 0, 20, 1, 0})
	require.Error(t, err)
	_, _, err = ExtractSNI(make([]byte, 64*1024+1))
	require.ErrorContains(t, err, "64 KiB")
}

func clientHello(t *testing.T, serverName string, ech bool) []byte {
	t.Helper()
	body := []byte{3, 3}
	body = append(body, make([]byte, 32)...)
	body = append(body, 0)
	body = append(body, 0, 2, 0x13, 0x01)
	body = append(body, 1, 0)
	var extensions []byte
	if serverName != "" {
		entry := []byte{0, byte(len(serverName) >> 8), byte(len(serverName))}
		entry = append(entry, []byte(serverName)...)
		list := append([]byte{byte(len(entry) >> 8), byte(len(entry))}, entry...)
		extensions = append(extensions, 0, 0, byte(len(list)>>8), byte(len(list)))
		extensions = append(extensions, list...)
	}
	if ech {
		extensions = append(extensions, 0xfe, 0x0d, 0, 1, 0)
	}
	body = append(body, byte(len(extensions)>>8), byte(len(extensions)))
	body = append(body, extensions...)
	handshake := []byte{1, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	return append(handshake, body...)
}

func tlsRecord(handshake []byte) []byte {
	record := make([]byte, 5, len(handshake)+5)
	record[0] = 22
	record[1] = 3
	record[2] = 3
	binary.BigEndian.PutUint16(record[3:5], uint16(len(handshake)))
	return append(record, handshake...)
}
