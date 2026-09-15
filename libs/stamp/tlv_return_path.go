package stamp

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type ReturnPathSubTLVType uint8

const (
	RPControlCode ReturnPathSubTLVType = 1
)

type SRExtReturnPath struct {
	ControlCode *SRExtReturnPathControlCode

	// Not supported
	// ReturnAddress *SRExtReturnPathReturnAddress
	// ReturnPathSegmentList *SRExtReturnPathSegmentList
}

func (p *SRExtReturnPath) Encode() ([]byte, error) {
	buf := make([]byte, 0, 64)
	buf = append(buf, 0x00, uint8(TLVReturnPath))
	buf = binary.BigEndian.AppendUint16(buf, 0) // length; placeholder
	beforeSubTLVLength := len(buf)              // Buffer size before adding sub-TLVs; used to calculate length

	if p.ControlCode != nil {
		controlCodeByte, err := p.ControlCode.Encode()
		if err != nil {
			return nil, err
		}

		buf = append(buf, controlCodeByte...)
	}

	binary.BigEndian.PutUint16(buf[beforeSubTLVLength-2:], uint16(len(buf)-beforeSubTLVLength))

	return buf, nil
}

//	0                   1                   2                   3
//	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |STAMP TLV Flags|   Type=1      |         Length=4              |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                   Control Code Flags                          |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type SRExtReturnPathControlCode struct {
	RequestReply bool // Bit 31; LSB
}

func (s *SRExtReturnPathControlCode) Encode() ([]byte, error) {
	var controlFlag uint32

	if s.RequestReply {
		controlFlag |= 0x1
	}

	buf := make([]byte, 8)
	buf[0] = 0x0
	buf[1] = uint8(RPControlCode)
	binary.BigEndian.PutUint16(buf[2:4], 4)
	binary.BigEndian.PutUint32(buf[4:8], controlFlag)

	return buf, nil
}

func decodeReturnPath(b []byte) (*SRExtReturnPath, error) {
	returnPath := &SRExtReturnPath{}

	cursor := 0
	for cursor < len(b) {
		if len(b)-cursor < 4 {
			return nil, errors.New("truncated return path sub-TLV header")
		}

		subType := ReturnPathSubTLVType(b[cursor+1])
		subLength := int(binary.BigEndian.Uint16(b[cursor+2 : cursor+4]))

		if cursor+4+subLength > len(b) {
			return nil, fmt.Errorf("return path sub-TLV type %d declares %d octets, %d remain", subType, subLength, len(b)-cursor-4)
		}

		val := b[cursor+4 : cursor+4+subLength]

		switch subType {
		case RPControlCode:
			controlCode, err := decodeControlCode(val)
			if err != nil {
				return nil, err
			}

			returnPath.ControlCode = controlCode

		default:
		}

		cursor += 4 + subLength
	}

	return returnPath, nil
}

func decodeControlCode(b []byte) (*SRExtReturnPathControlCode, error) {
	if len(b) != 4 {
		return nil, fmt.Errorf("control code sub-TLV is %d octets, need 4", len(b))
	}

	controlFlag := binary.BigEndian.Uint32(b)

	return &SRExtReturnPathControlCode{
		RequestReply: controlFlag&0x1 != 0,
	}, nil
}
