// unused for now: was created for encapping STAMP packets towards destination
// figured that seg6local actions do not apply to self-originated packets
// will be used in future for multi-hop tracing
package srv6

import (
	"errors"
	"net/netip"
	"syscall"

	"golang.org/x/sys/unix"
)

func EncodeSRH(segments []netip.Addr) ([]byte, error) {
	if len(segments) == 0 {
		return nil, errors.New("no SR segments")
	}

	n := len(segments) + 1
	buf := make([]byte, 8+16*n)

	buf[1] = uint8(2 * n)
	buf[2] = 4
	buf[3] = uint8(n - 1)
	buf[4] = uint8(n - 1)

	for i, s := range segments {
		if !s.Is6() {
			return nil, errors.New("SR segment is not ipv6")
		}

		copy(buf[8+16*(n-1-i):], s.AsSlice())
	}

	return buf, nil
}

func SRHControl(segments []netip.Addr) (func(string, string, syscall.RawConn) error, error) {
	srh, err := EncodeSRH(segments)
	if err != nil {
		return nil, err
	}

	return func(network, address string, c syscall.RawConn) error {
		var sockErr error

		if err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptString(int(fd), unix.IPPROTO_IPV6, unix.IPV6_RTHDR, string(srh))
		}); err != nil {
			return err
		}

		return sockErr

	}, nil
}
