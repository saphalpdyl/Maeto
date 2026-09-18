package stamp

import (
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// Packet format for unauth mode ( RFC 8762 )
//     0                   1                   2                   3
//     0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//    +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//    |                        Sequence Number                        |
//    +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//    |                          Timestamp                            |
//    |                                                               |
//    +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//    |         Error Estimate        |                               |
//    +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               +
//    |                                                               |
//    |                                                               |
//    |                        MBZ  (30 octets)                       |
//    |                                                               |
//    |                                                               |
//    |                                                               |
//    |                                                               |
//    +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

// SenderConfig holds the parameters needed to construct a Sender.
type SenderConfig struct {
	LocalAddr  string
	RemoteAddr string
	HMACKey    []byte
	OnError    func(error)
	Timeout    time.Duration
	Config     Config

	SRExtensions *SRExtensions
}

// Sender originates STAMP test packets and matches reflected replies against
// outstanding sequence numbers.
type Sender struct {
	Conn    *net.UDPConn
	HMACKey []byte
	seq     uint32
	onError func(error)
	timeout time.Duration
	Config  Config

	srExtensions *SRExtensions
}

// NewSender resolves the local and remote UDP addresses from cfg, opens a
// connected UDP socket, and returns a ready-to-use Sender.
func NewSender(cfg SenderConfig) (*Sender, error) {
	if cfg.Config.ErrorEstimate.ClockFormat != ClockFormatNTP {
		return nil, errors.New("invalid clock format: valid options are NTP")
	}

	localAddr, err := net.ResolveUDPAddr("udp", cfg.LocalAddr)
	if err != nil {
		return nil, err
	}

	remoteAddr, err := net.ResolveUDPAddr("udp", cfg.RemoteAddr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", localAddr, remoteAddr)
	if err != nil {
		return nil, err
	}

	if cfg.Config.BindToDev != nil {

		rawConn, err := conn.SyscallConn()
		if err != nil {
			err := conn.Close()
			if err != nil {
				return nil, errors.New("connection failed to close when handling failure for rawConn")
			}
			return nil, err
		}

		var sockErr error
		err = rawConn.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptString(
				int(fd),
				unix.SOL_SOCKET,
				unix.SO_BINDTODEVICE,
				*cfg.Config.BindToDev,
			)

		})
		if err != nil {
			err := conn.Close()
			if err != nil {
				return nil, errors.New("connection failed to close when handling failure for rawConn.Control")
			}
			return nil, err
		}

		if sockErr != nil {
			err := conn.Close()
			if err != nil {
				return nil, errors.New("connection failed to close when handling failure for SO_BINDTODEVICE")
			}
			return nil, sockErr
		}
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	return &Sender{
		Conn:         conn,
		HMACKey:      cfg.HMACKey,
		seq:          0,
		onError:      cfg.OnError,
		timeout:      timeout,
		Config:       cfg.Config,
		srExtensions: cfg.SRExtensions,
	}, nil
}

// Send transmits a single STAMP test packet (RFC 8762 §3.2) and blocks
// until the reflected response arrives or the connection's read deadline
// is exceeded.
//
// On success it returns the decoded ReflectorPacket containing the
// reflector's receive/send timestamps and the echoed sender fields.
// The internal sequence number is incremented after each successful send.
//
// Only unauthenticated mode is currently supported; Send panics if
// HMACKey is set.
func (s *Sender) Send() (*ReflectorPacket, error) {
	if s.HMACKey != nil {
		return nil, fmt.Errorf("HMAC authentication is not yet implemented")
	}

	buf, err := s.encodeNext()
	if err != nil {
		return nil, err
	}

	_, err = s.Conn.Write(buf)
	if err != nil {
		return nil, err
	}

	// Wait for the reflected reply
	_ = s.Conn.SetReadDeadline(time.Now().Add(s.timeout))
	rxBuf := make([]byte, 1500)
	n, err := s.Conn.Read(rxBuf)
	if err != nil {
		return nil, fmt.Errorf("waiting for reply: %w", err)
	}

	if n < 44 {
		return nil, fmt.Errorf("reply too short: got %d bytes, need 44", n)
	}

	reply, err := DecodeReflectorPacket(nil, rxBuf[:n])
	if err != nil {
		return nil, fmt.Errorf("decoding reply: %w", err)
	}

	s.seq++

	return reply, nil
}

// SendOnly transmits a single STAMP test packet without waiting for a
// reflected reply. It is used by one-way probes, where the far end exports its
// own receive timestamp out of band instead of reflecting the packet.
func (s *Sender) SendOnly() error {
	if s.HMACKey != nil {
		return fmt.Errorf("HMAC authentication is not yet implemented")
	}

	buf, err := s.encodeNext()
	if err != nil {
		return err
	}

	if _, err := s.Conn.Write(buf); err != nil {
		return err
	}

	s.seq++

	return nil
}

func (s *Sender) encodeNext() ([]byte, error) {
	timestamp, err := NewTimestamp(TimestampParams{
		ClockFormat: s.Config.ErrorEstimate.ClockFormat,
	})
	if err != nil {
		return nil, err
	}

	errorEstimate, err := NewErrorEstimate(
		s.Config.ErrorEstimate.Synchronized,
		s.Config.ErrorEstimate.ClockFormat,
		s.Config.ErrorEstimate.Scale,
		s.Config.ErrorEstimate.Multiplier,
	)
	if err != nil {
		return nil, err
	}

	senderPkt := SenderPacket{
		SequenceNumber: s.seq,
		Timestamp:      *timestamp,
		ErrorEstimate:  errorEstimate.Encode(),
		SRExtensions:   s.srExtensions,
	}

	return senderPkt.Encode(nil)
}

// Sequence returns the sequence number the next transmitted packet will carry.
func (s *Sender) Sequence() uint32 {
	return s.seq
}

// Close releases the underlying socket.
func (s *Sender) Close() error {
	if s.Conn == nil {
		return nil
	}
	_ = s.Conn.SetReadDeadline(time.Now())
	return s.Conn.Close()
}
