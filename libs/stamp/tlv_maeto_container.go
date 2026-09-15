package stamp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf8"
)

//	0                  1                  2                  3
//	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |STAMP TLV Flags|    Type=252   |             Length            |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// :                        Maeto Container                        :
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type SRExtMaetoContainer struct {
	TelemetryKey *SRExtMaetoContainerTelemetryKey
}

func (s *SRExtMaetoContainer) Encode() ([]byte, error) {
	buf := make([]byte, 4, 64)
	buf[0] = 0x0
	buf[1] = uint8(TLVMaetoContainer)
	binary.BigEndian.PutUint16(buf[2:], 0)

	beforeSubTLVLength := len(buf)

	if s.TelemetryKey != nil {
		enc, err := s.TelemetryKey.Encode()
		if err != nil {
			return nil, err
		}

		buf = append(buf, enc...)
	}

	length := len(buf) - beforeSubTLVLength

	if length > 0xFFFF {
		return nil, errors.New("maeto container length too big to fit in 16-bit length field")
	}

	binary.BigEndian.PutUint16(buf[2:], uint16(length))
	return buf, nil
}

type MaetoContainerSubTLVType uint8

const (
	MCTelemetryKey MaetoContainerSubTLVType = 1
)

//	0                  1                  2                  3
//	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |STAMP TLV Flags|     Type=1    |             Length            |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// :                         Telemetry-Key                         :
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type SRExtMaetoContainerTelemetryKey struct {
	Key string
}

func (s *SRExtMaetoContainerTelemetryKey) Encode() ([]byte, error) {
	if len(s.Key) > 0xFFFF {
		return nil, errors.New("telemetry key too long for 16-bit length")
	}

	buf := make([]byte, 4, 4+len(s.Key))
	buf[0] = 0x0
	buf[1] = uint8(MCTelemetryKey)
	binary.BigEndian.PutUint16(buf[2:], uint16(len(s.Key)))
	buf = append(buf, s.Key...)

	return buf, nil
}

func decodeMaetoContainer(b []byte) (*SRExtMaetoContainer, error) {
	container := &SRExtMaetoContainer{}

	cursor := 0
	for cursor < len(b) {
		if len(b)-cursor < 4 {
			return nil, errors.New("truncated maeto container sub-TLV header")
		}

		subType := MaetoContainerSubTLVType(b[cursor+1])
		subLength := int(binary.BigEndian.Uint16(b[cursor+2 : cursor+4]))

		if cursor+4+subLength > len(b) {
			return nil, fmt.Errorf("maeto container sub-TLV type %d declares %d octets, %d remain", subType, subLength, len(b)-cursor-4)
		}

		val := b[cursor+4 : cursor+4+subLength]

		switch subType {
		case MCTelemetryKey:
			telemetryKey, err := decodeTelemetryKey(val)
			if err != nil {
				return nil, err
			}

			container.TelemetryKey = telemetryKey

		default:
		}

		cursor += 4 + subLength
	}

	return container, nil
}

func decodeTelemetryKey(b []byte) (*SRExtMaetoContainerTelemetryKey, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("telemetry key is not valid utf-8")
	}

	return &SRExtMaetoContainerTelemetryKey{Key: string(b)}, nil
}
