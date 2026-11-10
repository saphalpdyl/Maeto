package maetoagent

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

const DefaultMaetoSocketPath = "/run/maeto/frr.bfd.sock"

type BFDListener struct {
	socketPath string
	ln         *net.UnixListener
	logger     *slog.Logger

	reportChan chan BFDMessage
}

func NewBFD(
	socketPath string,
	reportChan chan BFDMessage,
	logger *slog.Logger,
) *BFDListener {
	return &BFDListener{
		socketPath: socketPath,
		logger:     logger,
		reportChan: reportChan,
	}
}

func (b *BFDListener) Start(ctx context.Context) (err error) {
	_ = os.Remove(b.socketPath)

	b.ln, err = net.ListenUnix("unixpacket", &net.UnixAddr{Name: b.socketPath, Net: "unixpacket"})
	if err != nil {
		return err
	}

	// bfdd runs under frr cgroup; agent runs under root cgroup
	//
	// Without adding ownership, bfdd-module would not be able to send UNIX packets
	//		to agent-created frr.bfd.sock
	g, err := user.LookupGroup("frr")
	if err != nil {
		return fmt.Errorf("frr: could not lookup group: %v", err)
	}

	frrGID, err := strconv.Atoi(g.Gid)
	if err != nil {
		return fmt.Errorf("frr: could not convert group GID: %v", err)
	}

	err = os.Chown(b.socketPath, -1, frrGID)
	if err != nil {
		return fmt.Errorf("frr: could not change owner: %v", err)
	}

	err = os.Chmod(b.socketPath, 0o0660) //nolint:gosec // bfdd connects as group frr
	if err != nil {
		return fmt.Errorf("frr: could not change permissions: %v", err)
	}

	defer func(conn *net.UnixListener) {
		err := conn.Close()
		if err != nil {
			b.logger.Warn("cleanup: failed to close socket")
		}
	}(b.ln)

	go func() {
		<-ctx.Done()
		if b.ln != nil {
			_ = b.ln.Close()
		}
	}()

	for {
		conn, err := b.ln.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}

		b.logger.InfoContext(ctx, "bfdd connected")

		b.serve(ctx, conn)

		b.logger.InfoContext(ctx, "bfdd disconnected")
		b.report(ctx, BFDMessage{Type: BFDMessageDisconnected})
	}
}

func (b *BFDListener) serve(ctx context.Context, conn *net.UnixConn) {
	defer func() { _ = conn.Close() }()

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	buf := make([]byte, 65536)

	for {
		n, _, flags, _, err := conn.ReadMsgUnix(buf, nil)
		if err != nil || n == 0 {
			return // EOF (bfdd disconnected)
		}

		if flags&syscall.MSG_TRUNC != 0 {
			b.logger.ErrorContext(ctx, "bfdd truncated; buffer is too small for this message")
			return
		}

		fmt.Printf("bfdd message: %s\n", string(buf[:n]))

		message, err := ParseBFDMessage(buf[:n])
		if err != nil {
			b.logger.ErrorContext(ctx, "failed to parse BFD message", log.Err(err))
			return
		}

		if !b.report(ctx, message) {
			return
		}
	}
}

func (b *BFDListener) report(ctx context.Context, m BFDMessage) bool {
	select {
	case b.reportChan <- m:
		return true
	case <-ctx.Done():
		return false
	}
}
