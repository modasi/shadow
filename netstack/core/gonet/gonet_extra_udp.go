package gonet

import (
	"net"
)

func (c *UDPConn) WriteFrom(b []byte, addr net.Addr) (int, error) {
	n, err := c.WriteTo(b, nil)
	return n, err
}

func (c *UDPConn) ReadTo(b []byte) (int, net.Addr, error) {
	n, _, err := c.ReadFrom(b)
	return n, c.LocalAddr(), err
}
