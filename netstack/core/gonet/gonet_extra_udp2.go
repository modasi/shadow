package gonet

import (
	"errors"
	"fmt"
	"io"
	"net"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/ports"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

type UDPConn2 struct {
	deadlineTimer

	Delete func(string)
	stack  *stack.Stack
	route  []*stack.Route
	closed chan struct{}
	stream chan Packet
	addr   net.UDPAddr
	raddr  net.UDPAddr
	nicID  tcpip.NICID
	unique uint64
	tranID []stack.TransportEndpointID
	flags  ports.Flags
}

func NewUDPConn2(rr *ForwarderRequest) *UDPConn2 {
	conn := &UDPConn2{
		stack:  rr.Stack,
		closed: make(chan struct{}),
		stream: make(chan Packet, 10),
		addr: net.UDPAddr{
			IP:   net.IP(rr.ID.LocalAddress),
			Port: int(rr.ID.LocalPort),
		},
		raddr: net.UDPAddr{
			IP:   net.IP(rr.ID.RemoteAddress),
			Port: int(rr.ID.RemotePort),
		},
		nicID:  rr.Route.NICID(),
		unique: rr.Stack.UniqueID(),
	}
	conn.deadlineTimer.init()
	return conn
}

func (ep *UDPConn2) HandleRequest(rr *ForwarderRequest) error {
	ep.route = append(ep.route, rr.Route)
	ep.tranID = append(ep.tranID, rr.ID)
	if tcperr := rr.Stack.RegisterTransportEndpoint(rr.Route.NICID(), []tcpip.NetworkProtocolNumber{rr.Route.NetProto}, udp.ProtocolNumber, rr.ID, ep, ep.flags, rr.Route.NICID()); tcperr != nil {
		return errors.New(tcperr.String())
	}
	return nil
}

func (ep *UDPConn2) UniqueID() uint64 {
	return ep.unique
}

func (ep *UDPConn2) HandlePacket(r *stack.Route, id stack.TransportEndpointID, pkt *stack.PacketBuffer) {
	// Get the header then trim it from the view.
	hdr := header.UDP(pkt.TransportHeader)
	if int(hdr.Length()) > pkt.Data.Size()+header.UDPMinimumSize {
		// Malformed packet.
		ep.stack.Stats().UDP.MalformedPacketsReceived.Increment()
		return
	}
	ep.stack.Stats().UDP.PacketsReceived.Increment()

	packet := Packet{
		Addr: &net.UDPAddr{
			IP:   net.IP(id.LocalAddress),
			Port: int(id.LocalPort),
		},
		View: pkt.Data.ToView(),
	}

	select {
	case <-ep.closed:
	case ep.stream <- packet:
	}
}

func (ep *UDPConn2) HandleControlPacket(id stack.TransportEndpointID, typ stack.ControlType, extra uint32, pkt *stack.PacketBuffer) {}

func (ep *UDPConn2) Abort() {}

func (ep *UDPConn2) Wait() {}

func (ep *UDPConn2) closeEndpoint() {
	for i := range ep.route {
		ep.stack.UnregisterTransportEndpoint(ep.nicID, []tcpip.NetworkProtocolNumber{ep.route[i].NetProto}, udp.ProtocolNumber, ep.tranID[i], ep, ep.flags, ep.nicID)
		ep.stack.ReleasePort([]tcpip.NetworkProtocolNumber{ep.route[i].NetProto}, udp.ProtocolNumber, ep.tranID[i].LocalAddress, ep.tranID[i].LocalPort, ep.flags, ep.nicID, tcpip.FullAddress{})
		ep.route[i].Release()
	}
}

func (c *UDPConn2) Close() error {
	select {
	case <-c.closed:
		return nil
	default:
		close(c.closed)
	}

	c.closeEndpoint()
	c.Delete(c.raddr.String())
	return nil
}

func (c *UDPConn2) LocalAddr() net.Addr {
	return &c.addr
}

func (c *UDPConn2) Read(b []byte) (int, error) {
	n, _, err := c.ReadTo(b)
	return n, err
}

func (c *UDPConn2) ReadFrom(b []byte) (int, net.Addr, error) {
	n, _, err := c.ReadTo(b)
	return n, &c.raddr, err
}

func (c *UDPConn2) ReadTo(b []byte) (int, net.Addr, error) {
	deadline := c.readCancel()
	select {
	case <-deadline:
		return 0, nil, &timeoutError{}
	case packet := <-c.stream:
		n := copy(b, packet.View)
		if n < len(packet.View) {
			return n, packet.Addr, io.ErrShortBuffer
		}
		return n, packet.Addr, nil
	case <-c.closed:
		return 0, nil, io.EOF
	}
	return 0, nil, nil
}

func (c *UDPConn2) RemoteAddr() net.Addr {
	return &c.raddr
}

func (c *UDPConn2) Write(b []byte) (int, error) {
	return 0, fmt.Errorf("no local address")
}

func (c *UDPConn2) WriteTo(b []byte, addr net.Addr) (int, error) {
	return 0, errors.New("no local address")
}

func (c *UDPConn2) WriteFrom(b []byte, addr net.Addr) (int, error) {
	deadline := c.writeCancel()
	select {
	case <-deadline:
		return 0, &timeoutError{}
	default:
	}

	view := buffer.View(b)
	data := view.ToVectorisedView()
	uaddr := addr.(*net.UDPAddr)
	route := c.route[0].Clone()
	route.LocalAddress = tcpip.Address(uaddr.IP)

	err := SendPacket(&route, data, uint16(uaddr.Port), uint16(c.raddr.Port))
	return len(b), err
}
