package namecapture

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const maxClientHelloBytes = 64 * 1024

func ExtractSNI(payload []byte) (string, bool, error) {
	if len(payload) > maxClientHelloBytes {
		return "", false, fmt.Errorf("TLS ClientHello exceeds 64 KiB")
	}
	var handshake []byte
	for offset := 0; offset < len(payload); {
		if len(payload)-offset < 5 {
			return "", false, fmt.Errorf("truncated TLS record header")
		}
		length := int(binary.BigEndian.Uint16(payload[offset+3 : offset+5]))
		offset += 5
		if length > len(payload)-offset {
			return "", false, fmt.Errorf("truncated TLS record body")
		}
		if payload[offset-5] == 22 {
			handshake = append(handshake, payload[offset:offset+length]...)
			if len(handshake) > maxClientHelloBytes {
				return "", false, fmt.Errorf("TLS ClientHello exceeds 64 KiB")
			}
		}
		offset += length
	}
	if len(handshake) < 4 {
		return "", false, fmt.Errorf("missing TLS handshake header")
	}
	if handshake[0] != 1 {
		return "", false, nil
	}
	handshakeLength := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
	if handshakeLength > maxClientHelloBytes-4 {
		return "", false, fmt.Errorf("TLS ClientHello exceeds 64 KiB")
	}
	if len(handshake) < 4+handshakeLength {
		return "", false, fmt.Errorf("truncated TLS ClientHello")
	}
	return parseClientHello(handshake[4 : 4+handshakeLength])
}

func parseClientHello(body []byte) (string, bool, error) {
	offset := 2 + 32
	if len(body) < offset+1 {
		return "", false, fmt.Errorf("truncated TLS ClientHello random")
	}
	sessionLength := int(body[offset])
	offset++
	if len(body) < offset+sessionLength+2 {
		return "", false, fmt.Errorf("truncated TLS ClientHello session")
	}
	offset += sessionLength
	cipherLength := int(binary.BigEndian.Uint16(body[offset : offset+2]))
	offset += 2
	if cipherLength == 0 || cipherLength%2 != 0 || len(body) < offset+cipherLength+1 {
		return "", false, fmt.Errorf("invalid TLS ClientHello cipher suites")
	}
	offset += cipherLength
	compressionLength := int(body[offset])
	offset++
	if len(body) < offset+compressionLength {
		return "", false, fmt.Errorf("truncated TLS ClientHello compression")
	}
	offset += compressionLength
	if offset == len(body) {
		return "", false, nil
	}
	if len(body) < offset+2 {
		return "", false, fmt.Errorf("truncated TLS ClientHello extensions")
	}
	extensionsLength := int(binary.BigEndian.Uint16(body[offset : offset+2]))
	offset += 2
	if extensionsLength > len(body)-offset {
		return "", false, fmt.Errorf("truncated TLS ClientHello extensions")
	}
	end := offset + extensionsLength
	for offset < end {
		if end-offset < 4 {
			return "", false, fmt.Errorf("truncated TLS extension header")
		}
		extensionType := binary.BigEndian.Uint16(body[offset : offset+2])
		extensionLength := int(binary.BigEndian.Uint16(body[offset+2 : offset+4]))
		offset += 4
		if extensionLength > end-offset {
			return "", false, fmt.Errorf("truncated TLS extension body")
		}
		if extensionType == 0 {
			return parseServerName(body[offset : offset+extensionLength])
		}
		offset += extensionLength
	}
	return "", false, nil
}

func parseServerName(extension []byte) (string, bool, error) {
	if len(extension) < 2 {
		return "", false, fmt.Errorf("truncated TLS server name list")
	}
	listLength := int(binary.BigEndian.Uint16(extension[:2]))
	if listLength != len(extension)-2 {
		return "", false, fmt.Errorf("invalid TLS server name list length")
	}
	for offset := 2; offset < len(extension); {
		if len(extension)-offset < 3 {
			return "", false, fmt.Errorf("truncated TLS server name")
		}
		nameType := extension[offset]
		nameLength := int(binary.BigEndian.Uint16(extension[offset+1 : offset+3]))
		offset += 3
		if nameLength > len(extension)-offset {
			return "", false, fmt.Errorf("truncated TLS server name value")
		}
		if nameType == 0 {
			name := strings.ToLower(strings.TrimSuffix(string(extension[offset:offset+nameLength]), "."))
			if !validServerName(name) {
				return "", false, fmt.Errorf("invalid TLS server name")
			}
			return name, true, nil
		}
		offset += nameLength
	}
	return "", false, nil
}

func validServerName(name string) bool {
	if name == "" || len(name) > 253 || strings.ContainsAny(name, " \t\r\n\x00") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}
