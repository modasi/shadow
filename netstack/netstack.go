package netstack

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/imgk/shadow/netstack/core"
	"github.com/imgk/shadow/utils"
)

type Device interface {
	io.Reader
	io.WriterTo
	io.Writer
	io.ReaderFrom
	io.Closer
}

type Handler interface {
	Handle(net.Conn, net.Addr) error
	HandlePacket(PacketConn) error
}

type PacketConn interface {
	Close() error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SetDeadline(time.Time) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	ReadTo([]byte) (int, net.Addr, error)
	WriteFrom([]byte, net.Addr) (int, error)
}

type UDPConn struct {
	core.PacketConn
	target net.Addr
}

func NewUDPConn(conn core.PacketConn, tgt net.Addr) UDPConn {
	return UDPConn{
		PacketConn: conn,
		target:     tgt,
	}
}

func (conn UDPConn) ReadTo(b []byte) (int, net.Addr, error) {
	n, _, err := conn.PacketConn.ReadFrom(b)
	return n, conn.target, err
}

func (conn UDPConn) WriteFrom(b []byte, addr net.Addr) (int, error) {
	return conn.PacketConn.WriteTo(b, nil)
}

type UDPConn2 struct {
	core.PacketConn
	stack *Stack
}

func NewUDPConn2(conn core.PacketConn, s *Stack) UDPConn2 {
	return UDPConn2{
		PacketConn: conn,
		stack:      s,
	}
}

func (conn UDPConn2) ReadTo(b []byte) (int, net.Addr, error) {
	n, addr, err := conn.PacketConn.ReadTo(b)
	addr, _ = conn.stack.LookupAddr(addr)
	return n, addr, err
}

func (conn UDPConn2) WriteFrom(data []byte, src net.Addr) (int, error) {
	if addr, ok := src.(utils.Addr); ok {
		src, err := utils.ResolveUDPAddr(addr)
		if err != nil {
			return 0, fmt.Errorf("resolve udp addr error: %w", err)
		}
		return conn.PacketConn.WriteFrom(data, src)
	}

	return conn.PacketConn.WriteFrom(data, src)
}
