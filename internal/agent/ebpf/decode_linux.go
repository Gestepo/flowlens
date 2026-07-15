//go:build linux

package ebpf

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

const rawEventSize = 80

func decodeRecord(record []byte) (Observation, error) {
	if len(record) != rawEventSize {
		return Observation{}, fmt.Errorf("record size must be %d, got %d", rawEventSize, len(record))
	}

	family := record[20]
	local, remote, err := decodeAddresses(family, record[28:44], record[44:60])
	if err != nil {
		return Observation{}, err
	}
	return Observation{
		MonotonicNS: binary.LittleEndian.Uint64(record[0:8]),
		CgroupID:    binary.LittleEndian.Uint64(record[8:16]),
		PID:         binary.LittleEndian.Uint32(record[16:20]),
		Protocol:    record[21],
		State:       record[22],
		LocalPort:   binary.BigEndian.Uint16(record[24:26]),
		RemotePort:  binary.BigEndian.Uint16(record[26:28]),
		LocalIP:     local,
		RemoteIP:    remote,
		Sent:        binary.LittleEndian.Uint64(record[64:72]),
		Received:    binary.LittleEndian.Uint64(record[72:80]),
	}, nil
}

func decodeAddresses(family byte, local, remote []byte) (netip.Addr, netip.Addr, error) {
	switch family {
	case 2:
		return netip.AddrFrom4([4]byte(local[:4])), netip.AddrFrom4([4]byte(remote[:4])), nil
	case 10:
		return netip.AddrFrom16([16]byte(local)), netip.AddrFrom16([16]byte(remote)), nil
	default:
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("unsupported address family %d", family)
	}
}
