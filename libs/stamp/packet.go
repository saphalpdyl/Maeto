package stamp

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// SenderPacket is an unauthenticated STAMP sender test packet (RFC 8762 §3.2).
// The wire format is 44 octets: sequence number, timestamp, error estimate,
// and 30 octets of MBZ (must be zero) padding.
type SenderPacket struct {
	SequenceNumber uint32
	Timestamp      Timestamp
	ErrorEstimate  uint16
	SSID           uint16
	SRExtensions   *SRExtensions
}

type ReturnPathSubTLVType uint8

const (
	RPControlCode ReturnPathSubTLVType = 1
)

type STAMPTLVType uint8

const (
	TLVReturnPath STAMPTLVType = 10
)

type SRExtensions struct {
	ReturnPath *SRExtReturnPath
}

func (p *SRExtensions) Encode() ([]byte, error) {
	buf := make([]byte, 0, 32)

	if p.ReturnPath != nil {
		enc, err := p.ReturnPath.Encode()
		if err != nil {
			return nil, err
		}

		buf = append(buf, enc...)
	}

	return buf, nil
}

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

// Encode serializes the SenderPacket into a 44-octet buffer.
// hmacKey must be nil; authenticated mode is not yet supported.
func (p *SenderPacket) Encode(hmacKey []byte) ([]byte, error) {
	if hmacKey != nil {
		// Auth mode not supported
		return nil, errors.New("authenticated mode not implemented")
	}

	buf := make([]byte, 44)

	binary.BigEndian.PutUint32(buf[0:4], p.SequenceNumber)
	binary.BigEndian.PutUint32(buf[4:8], p.Timestamp.Seconds)
	binary.BigEndian.PutUint32(buf[8:12], p.Timestamp.Fraction)
	binary.BigEndian.PutUint16(buf[12:14], p.ErrorEstimate)
	binary.BigEndian.PutUint16(buf[14:16], p.SSID)

	if p.SRExtensions != nil {
		enc, err := p.SRExtensions.Encode()
		if err != nil {
			return nil, err
		}

		buf = append(buf, enc...)
	}

	return buf, nil
}

// DecodeSenderPacket parses a 44-octet buffer into a SenderPacket.
// hmacKey must be nil; authenticated mode is not yet supported.
func DecodeSenderPacket(hmacKey []byte, b []byte) (*SenderPacket, error) {
	if hmacKey != nil {
		// Auth mode not supported
		panic("not implemented")
	}

	if len(b) < 44 {
		return nil, errors.New("packet too short")
	}

	p := &SenderPacket{
		SequenceNumber: binary.BigEndian.Uint32(b[0:4]),
		Timestamp: Timestamp{
			Seconds:  binary.BigEndian.Uint32(b[4:8]),
			Fraction: binary.BigEndian.Uint32(b[8:12]),
		},
		ErrorEstimate: binary.BigEndian.Uint16(b[12:14]),
		SSID:          binary.BigEndian.Uint16(b[14:16]),
	}

	if len(b) == 44 {
		return p, nil
	}

	ext := &SRExtensions{}

	cursor := 44
	for cursor < len(b) {
		if len(b)-cursor < 4 {
			return nil, errors.New("truncated TLV header")
		}

		tlvType := STAMPTLVType(b[cursor+1])
		tlvLength := int(binary.BigEndian.Uint16(b[cursor+2 : cursor+4]))

		if cursor+4+tlvLength > len(b) {
			return nil, fmt.Errorf("TLV type %d declares %d octets, %d remain", tlvType, tlvLength, len(b)-cursor-4)
		}

		val := b[cursor+4 : cursor+4+tlvLength]

		switch tlvType {
		case TLVReturnPath:
			returnPath, err := decodeReturnPath(val)
			if err != nil {
				return nil, err
			}

			ext.ReturnPath = returnPath

		default:
			// TODO: Report unrecognized TLV instead
		}

		cursor += 4 + tlvLength
	}

	p.SRExtensions = ext

	return p, nil
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

// ReflectorPacket is an unauthenticated STAMP reflector response packet
// (RFC 8762 §4.2). It echoes the sender's fields and adds the reflector's
// own timestamps, totalling 44 octets on the wire.
type ReflectorPacket struct {
	SequenceNumber       uint32
	Timestamp            Timestamp
	ErrorEstimate        uint16
	ReceiveTimestamp     Timestamp
	SenderSequenceNumber uint32
	SenderTimestamp      Timestamp
	SenderErrorEstimate  uint16
	SenderTTL            uint8
	SSID                 uint16
}

// Encode serializes the ReflectorPacket into a 44-octet buffer.
// hmacKey must be nil; authenticated mode is not yet supported.
func (p *ReflectorPacket) Encode(hmacKey []byte) ([]byte, error) {
	if hmacKey != nil {
		// Auth mode not supported
		panic("not implemented")
	}

	buf := make([]byte, 44)

	binary.BigEndian.PutUint32(buf[0:4], p.SequenceNumber)
	binary.BigEndian.PutUint32(buf[4:8], p.Timestamp.Seconds)
	binary.BigEndian.PutUint32(buf[8:12], p.Timestamp.Fraction)
	binary.BigEndian.PutUint16(buf[12:14], p.ErrorEstimate)
	binary.BigEndian.PutUint16(buf[14:16], p.SSID)

	binary.BigEndian.PutUint32(buf[16:20], p.ReceiveTimestamp.Seconds)
	binary.BigEndian.PutUint32(buf[20:24], p.ReceiveTimestamp.Fraction)
	binary.BigEndian.PutUint32(buf[24:28], p.SenderSequenceNumber)
	binary.BigEndian.PutUint32(buf[28:32], p.SenderTimestamp.Seconds)
	binary.BigEndian.PutUint32(buf[32:36], p.SenderTimestamp.Fraction)
	binary.BigEndian.PutUint16(buf[36:38], p.SenderErrorEstimate)

	// 38-39: MBZ

	buf[40] = p.SenderTTL

	// 41-43: MBZ

	return buf, nil
}

// DecodeReflectorPacket parses a 44-octet buffer into a ReflectorPacket.
// hmacKey must be nil; authenticated mode is not yet supported.
func DecodeReflectorPacket(hmacKey []byte, data []byte) (*ReflectorPacket, error) {
	if hmacKey != nil {
		// Auth mode not supported
		panic("not implemented")
	}

	if len(data) < 44 {
		return nil, fmt.Errorf("packet too short: got %d bytes, need 44", len(data))
	}

	p := &ReflectorPacket{
		SequenceNumber: binary.BigEndian.Uint32(data[0:4]),
		Timestamp: Timestamp{
			Seconds:  binary.BigEndian.Uint32(data[4:8]),
			Fraction: binary.BigEndian.Uint32(data[8:12]),
		},
		ErrorEstimate: binary.BigEndian.Uint16(data[12:14]),
		SSID:          binary.BigEndian.Uint16(data[14:16]),
		ReceiveTimestamp: Timestamp{
			Seconds:  binary.BigEndian.Uint32(data[16:20]),
			Fraction: binary.BigEndian.Uint32(data[20:24]),
		},
		SenderSequenceNumber: binary.BigEndian.Uint32(data[24:28]),
		SenderTimestamp: Timestamp{
			Seconds:  binary.BigEndian.Uint32(data[28:32]),
			Fraction: binary.BigEndian.Uint32(data[32:36]),
		},
		SenderErrorEstimate: binary.BigEndian.Uint16(data[36:38]),
		// 38-39: MBZ
		SenderTTL: data[40],
		// 41-43: MBZ
	}

	return p, nil
}
