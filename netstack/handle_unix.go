// +build linux darwin

package netstack

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/imgk/shadow/netstack/core"
	"github.com/imgk/shadow/utils"
)

type Stack struct {
	core.Stack
	Pool     sync.Pool
	IPFilter *utils.IPFilter
	Tree     *utils.Tree
	Resolver utils.Resolver
	Handler  Handler
	counter  uint16
}

func NewStack(handler Handler, w io.Writer) *Stack {
	s := &Stack{
		Pool: sync.Pool{
			New: func() interface{} { return NewMessage() },
		},
		IPFilter: utils.NewIPFilter(),
		Tree:     utils.NewTree("."),
		Handler:  handler,
		counter:  uint16(time.Now().Unix()),
	}
	s.Stack.Init(w, s, false)
	return s
}

func (s *Stack) Close() error {
	s.Resolver.Stop()
	s.Stack.Close()
	return nil
}

func (s *Stack) Handle(conn core.Conn) {
	target := conn.LocalAddr().(*net.TCPAddr)

	if !s.IPFilter.Lookup(target.IP) {
		s.Info(fmt.Sprintf("direct %v <-TCP-> %v", conn.RemoteAddr(), target))
		if err := s.HandleTCP2(conn, target); err != nil {
			s.Error(fmt.Sprintf("handle tcp2 error: %v", err))
		}
		return
	}

	addr, err := s.LookupAddr(target)
	if err == ErrNotFake {
		s.Info(fmt.Sprintf("proxy %v <-TCP-> %v", conn.RemoteAddr(), target))
		if err := s.HandleTCP(conn, target); err != nil {
			s.Error(fmt.Sprintf("handle tcp error: %v", err))
		}
		return
	}
	if err == ErrNotFound {
		s.Error(fmt.Sprintf("handle tcp error: %v", err))
		conn.Close()
		return
	}

	s.Info(fmt.Sprintf("proxy %v <-TCP-> %v", conn.RemoteAddr(), addr))
	if err := s.HandleTCP(conn, addr); err != nil {
		s.Error(fmt.Sprintf("handle tcp error: %v", err))
	}
	return
}

func (s *Stack) PickConnType(addr net.UDPAddr) core.PacketConnType {
	if v4 := addr.IP.To4(); v4 != nil {
		if v4[0] == 198 && v4[1] == 18 {
			return core.ConnType
		}
	}

	if addr.Port == 53 && !s.IPFilter.Lookup(addr.IP) {
		return core.ConnType3
	}

	return core.ConnType2
}

func (s *Stack) HandlePacket(conn core.PacketConn) {
	target := conn.LocalAddr().(*net.UDPAddr)

	addr, err := s.LookupAddr(target)
	if err == ErrNotFound {
		conn.Close()
		return
	}

	pc := NewUDPConn(conn, addr)
	s.Info(fmt.Sprintf("proxy %v <-UDP-> %v", conn.RemoteAddr(), addr))
	if err := s.HandleUDP(pc); err != nil {
		s.Error(fmt.Sprintf("handle udp error: %v", err))
	}
	return
}

func (s *Stack) HandlePacket2(conn core.PacketConn) {
	target := conn.LocalAddr().(*net.UDPAddr)

	if !s.IPFilter.Lookup(target.IP) {
		s.Info(fmt.Sprintf("direct %v <-UDP-> 0.0.0.0:0", conn.RemoteAddr()))
		if err := s.HandleUDP2(conn); err != nil {
			s.Error(fmt.Sprintf("handle udp2 error: %v", err))
		}
		return
	}

	pc := NewUDPConn2(conn, s)
	s.Info(fmt.Sprintf("proxy %v <-UDP-> 0.0.0.0:0", conn.RemoteAddr()))
	if err := s.HandleUDP(pc); err != nil {
		s.Error(fmt.Sprintf("handle udp2 error: %v", err))
	}
}

func (s *Stack) HandlePacket3(conn core.PacketConn) {
	s.Info(fmt.Sprintf("hijack 0.0.0.0:0 <-UDP-> %v", conn.LocalAddr()))
	if err := s.HandleUDP3(conn); err != nil {
		s.Error(fmt.Sprintf("handle udp3 error: %v", err))
	}
}
