package proxy

import (
	"net"
	"testing"
)

func TestReadConnectResponseRejectsReservedByte(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go func() {
		_, _ = server.Write([]byte{0x05, 0x00, 0x01, 0x01, 0, 0, 0, 0, 0, 0})
		_ = server.Close()
	}()
	if err := readConnectResponse(client); err == nil {
		t.Fatal("readConnectResponse() accepted a nonzero reserved byte")
	}
}
