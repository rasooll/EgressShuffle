package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// Dialer establishes a destination connection through one SOCKS5 backend.
type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// SOCKS5Dialer implements unauthenticated SOCKS5 without resolving destination
// hostnames locally.
type SOCKS5Dialer struct {
	NetDialer net.Dialer
}

func (d SOCKS5Dialer) DialContext(ctx context.Context, backendAddress, targetAddress string) (net.Conn, error) {
	conn, err := d.NetDialer.DialContext(ctx, "tcp", backendAddress)
	if err != nil {
		return nil, fmt.Errorf("connect to SOCKS5 backend: %w", err)
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	succeeded := false
	defer func() {
		if !succeeded {
			stopCancel()
			_ = conn.Close()
		}
	}()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set SOCKS5 deadline: %w", err)
		}
	}

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return nil, fmt.Errorf("write SOCKS5 greeting: %w", err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		return nil, fmt.Errorf("read SOCKS5 greeting: %w", err)
	}
	if greeting[0] != 0x05 || greeting[1] != 0x00 {
		return nil, fmt.Errorf("SOCKS5 backend rejected authentication method")
	}

	request, err := connectRequest(targetAddress)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(request); err != nil {
		return nil, fmt.Errorf("write SOCKS5 connect request: %w", err)
	}
	if err := readConnectResponse(conn); err != nil {
		return nil, err
	}
	if !stopCancel() {
		return nil, fmt.Errorf("SOCKS5 connect canceled: %w", ctx.Err())
	}
	if err := conn.SetDeadline(noDeadline); err != nil {
		return nil, fmt.Errorf("clear SOCKS5 deadline: %w", err)
	}
	succeeded = true
	return conn, nil
}

var noDeadline = time.Time{}

func connectRequest(targetAddress string) ([]byte, error) {
	host, portText, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid destination address %q: %w", targetAddress, err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid destination port %q", portText)
	}

	request := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			request = append(request, 0x01)
			request = append(request, ip4...)
		} else {
			request = append(request, 0x04)
			request = append(request, ip.To16()...)
		}
	} else {
		if len(host) == 0 || len(host) > 255 {
			return nil, fmt.Errorf("destination hostname length must be between 1 and 255 bytes")
		}
		request = append(request, 0x03, byte(len(host)))
		request = append(request, host...)
	}
	return binary.BigEndian.AppendUint16(request, uint16(port)), nil
}

func readConnectResponse(conn net.Conn) error {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return fmt.Errorf("read SOCKS5 connect response: %w", err)
	}
	if header[0] != 0x05 {
		return fmt.Errorf("SOCKS5 backend returned unsupported version %d", header[0])
	}
	if header[1] != 0x00 {
		return fmt.Errorf("SOCKS5 connect rejected: %s", socksReply(header[1]))
	}
	if header[2] != 0x00 {
		return fmt.Errorf("SOCKS5 backend returned nonzero reserved byte")
	}

	addressLength := 0
	switch header[3] {
	case 0x01:
		addressLength = 4
	case 0x03:
		length := []byte{0}
		if _, err := io.ReadFull(conn, length); err != nil {
			return fmt.Errorf("read SOCKS5 bound hostname length: %w", err)
		}
		addressLength = int(length[0])
	case 0x04:
		addressLength = 16
	default:
		return fmt.Errorf("SOCKS5 backend returned unsupported address type %d", header[3])
	}
	if _, err := io.CopyN(io.Discard, conn, int64(addressLength+2)); err != nil {
		return fmt.Errorf("read SOCKS5 bound address: %w", err)
	}
	return nil
}

func socksReply(code byte) string {
	messages := map[byte]string{
		0x01: "general failure",
		0x02: "connection not allowed",
		0x03: "network unreachable",
		0x04: "host unreachable",
		0x05: "connection refused",
		0x06: "TTL expired",
		0x07: "command not supported",
		0x08: "address type not supported",
	}
	if message, ok := messages[code]; ok {
		return message
	}
	return fmt.Sprintf("unknown reply code %d", code)
}
