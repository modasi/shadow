package core

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	"go.uber.org/zap"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"

	"github.com/imgk/shadow/netstack/core/gonet"
)

type PacketConnType int

const (
	// symmetry NAT connection
	ConnType PacketConnType = iota
	// full cone NAT connection
	ConnType2
	// UDP Listener connection
	ConnType3
)

type Handler interface {
	Handle(Conn)
	PickConnType(net.UDPAddr) PacketConnType
	HandlePacket(PacketConn)
	HandlePacket2(PacketConn)
	HandlePacket3(PacketConn)
}

type Conn interface {
	Close() error
	CloseRead() error
	CloseWrite() error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SetDeadline(time.Time) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Read([]byte) (int, error)
	Write([]byte) (int, error)
}

type PacketConn interface {
	Close() error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SetDeadline(time.Time) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Read([]byte) (int, error)
	ReadTo([]byte) (int, net.Addr, error)
	ReadFrom([]byte) (int, net.Addr, error)
	Write([]byte) (int, error)
	WriteTo([]byte, net.Addr) (int, error)
	WriteFrom([]byte, net.Addr) (int, error)
}

type Stack struct {
	*zap.Logger
	Writer   io.Writer
	Handler  Handler
	Endpoint *channel.Endpoint
	Stack    *stack.Stack
	mutex    sync.Mutex
	conns    map[string]*gonet.UDPConn2
}

func (s *Stack) Init(device io.Writer, handler Handler, verbose bool) {
	const nicID = 1

	s.Logger, _ = zap.NewDevelopment()
	s.Writer = device
	s.Handler = handler
	s.Endpoint = channel.New(512, 1500, "")
	s.Endpoint.AddNotify(s)

	s.Stack = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocol{ipv4.NewProtocol(), ipv6.NewProtocol()},
		TransportProtocols: []stack.TransportProtocol{tcp.NewProtocol(), udp.NewProtocol()},
	})
	if err := s.Stack.CreateNIC(nicID, s.Endpoint); err != nil {
		s.Error(err.String())
		return
	}

	subnet, _ := tcpip.NewSubnet(tcpip.Address(strings.Repeat("\x00", 4)), tcpip.AddressMask(strings.Repeat("\x00", 4)))
	s.Stack.AddAddressRange(nicID, ipv4.ProtocolNumber, subnet)

	subnet, _ = tcpip.NewSubnet(tcpip.Address(strings.Repeat("\x00", 16)), tcpip.AddressMask(strings.Repeat("\x00", 16)))
	s.Stack.AddAddressRange(nicID, ipv6.ProtocolNumber, subnet)

	tcpFwd := tcp.NewForwarder(s.Stack, 32*1024, 1024, s.ForwardTCP)
	s.Stack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpFwd.HandlePacket)

	udpFwd := udp.NewForwarder(s.Stack, s.ForwardUDP)
	s.Stack.SetTransportProtocolHandler(udp.ProtocolNumber, udpFwd.HandlePacket)

	s.conns = make(map[string]*gonet.UDPConn2)
}

func (s *Stack) Write(b []byte) (int, error) {
	switch header.IPVersion(b) {
	case header.IPv4Version:
		s.Endpoint.InjectInbound(header.IPv4ProtocolNumber, &stack.PacketBuffer{Data: buffer.View(b).ToVectorisedView()})
	case header.IPv6Version:
		s.Endpoint.InjectInbound(header.IPv6ProtocolNumber, &stack.PacketBuffer{Data: buffer.View(b).ToVectorisedView()})
	}
	return len(b), nil
}

func (s *Stack) WriteNotify() {
	packet, ok := s.Endpoint.Read()
	if ok {
		size := packet.Pkt.Header.UsedLength() + packet.Pkt.Data.Size()
		view := []buffer.View{packet.Pkt.Header.View(), packet.Pkt.Data.ToView()}
		buff := buffer.NewVectorisedView(size, view)
		if _, err := s.Writer.Write(buff.ToView()); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			s.Error(fmt.Sprintf("write to tun error: %v", err))
		}
	}
}

func (s *Stack) Close() error {
	s.Stack.Close()
	s.Logger.Sync()
	return nil
}

func (s *Stack) ForwardTCP(r *tcp.ForwarderRequest) {
	wq := waiter.Queue{}
	ep, err := r.CreateEndpoint(&wq)
	if err != nil {
		s.Error("ForwardTCP error: " + err.String())
		r.Complete(true)
		return
	}
	r.Complete(false)

	go s.Handler.Handle(gonet.NewTCPConn(&wq, ep))
}

func (s *Stack) Get(k string) (*gonet.UDPConn2, bool) {
	s.mutex.Lock()
	conn, ok := s.conns[k]
	s.mutex.Unlock()
	return conn, ok
}

func (s *Stack) Add(k string, conn *gonet.UDPConn2) {
	s.mutex.Lock()
	s.conns[k] = conn
	s.mutex.Unlock()
}

func (s *Stack) Del(k string) {
	s.mutex.Lock()
	delete(s.conns, k)
	s.mutex.Unlock()
}

func (s *Stack) ForwardUDP(r *udp.ForwarderRequest) {
	rr := (*gonet.ForwarderRequest)(unsafe.Pointer(r))
	connType := s.Handler.PickConnType(net.UDPAddr{
		IP:   net.IP(rr.ID.LocalAddress),
		Port: int(rr.ID.LocalPort),
	})
	switch connType {
	case ConnType:
		conn, err := rr.CreateUDPConn()
		if err != nil {
			s.Error("ForwardUDP error: " + err.Error())
			return
		}

		go s.Handler.HandlePacket(conn)
		return
	case ConnType2:
		raddr := net.UDPAddr{
			IP:   net.IP(rr.ID.RemoteAddress),
			Port: int(rr.ID.RemotePort),
		}
		k := raddr.String()
		if conn, ok := s.Get(k); ok {
			if err := conn.HandleRequest(rr); err != nil {
				s.Error("ForwardUDP2 error: " + err.Error())
			}
			return
		}

		conn, err := rr.CreateUDPConn2()
		if err != nil {
			s.Error("ForwardUDP2 error: " + err.Error())
			return
		}
		conn.Delete = s.Del
		s.Add(k, conn)

		go s.Handler.HandlePacket2(conn)
		return
	case ConnType3:
		conn, err := rr.CreateUDPConn3()
		if err != nil {
			s.Error("ForwardUDP3 error: " + err.Error())
			return
		}

		go s.Handler.HandlePacket3(conn)
		return
	}
}
