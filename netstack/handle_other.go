// +build !windows

package netstack

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"

	"github.com/eycorsican/go-tun2socks/core"

	"github.com/imgk/shadow/log"
	"github.com/imgk/shadow/utils"
)

type stack struct {
	sync.RWMutex
	core.LWIPStack
	utils.Resolver
	*utils.IPFilter
	*utils.Tree
	Handler
	conns   map[core.UDPConn]PacketConn
	counter uint16
}

func NewStack(handler Handler, w io.Writer) *stack {
	s := &stack{
		RWMutex:   sync.RWMutex{},
		LWIPStack: core.NewLWIPStack(),
		IPFilter:  utils.NewIPFilter(),
		Tree:      utils.NewTree("."),
		Handler:   handler,
		conns:     make(map[core.UDPConn]PacketConn),
		counter:   uint16(time.Now().Unix()),
	}

	core.RegisterTCPConnHandler(s)
	core.RegisterUDPConnHandler(s)
	core.RegisterOutputFn(w.Write)

	return s
}

func (s *stack) Handle(conn net.Conn, target *net.TCPAddr) error {
	if !s.IPFilter.Lookup(target.IP) {
		log.Logf("direct %v <-TCP-> %v", conn.LocalAddr(), target)
		go s.RedirectTCP(conn, target)

		return nil
	}

	addr, err := s.IPToDomainAddrBuffer(target.IP, make([]byte, utils.MaxAddrLen))
	if err != nil {
		log.Logf("proxy %v <-TCP-> %v", conn.LocalAddr(), target)
		go s.HandleTCP(conn, target)

		return nil
	}
	binary.BigEndian.PutUint16(addr[len(addr)-2:], uint16(target.Port))

	log.Logf("proxy %v <-TCP-> %v", conn.LocalAddr(), addr)
	go s.HandleTCP(conn, addr)

	return nil
}

func (s *stack) Connect(conn core.UDPConn, target *net.UDPAddr) error {
	if target == nil {
		rc, err := net.ListenPacket("udp", "")
		if err != nil {
			log.Logf("listen packet conn error: %v", err)
			return err
		}

		pc := NewDirectUDPConn(conn, rc)
		s.Add(pc)

		log.Logf("direct %v <-UDP-> any", conn.LocalAddr())
		go s.RedirectUDP(pc)

		return nil
	}

	if target.Port == 53 {
		pc := NewUDPConn(conn, target, s)
		s.Add(pc)

		log.Logf("hijack %v <-UDP-> %v", conn.LocalAddr(), target)
		go s.HandleMessage(pc)

		return nil
	}

	if !s.IPFilter.Lookup(target.IP) {
		rc, err := net.ListenPacket("udp", "")
		if err != nil {
			log.Logf("listen packet conn error: %v", err)
			return err
		}

		pc := NewDirectUDPConn(conn, rc)
		s.Add(pc)

		log.Logf("direct %v <-UDP-> %v", conn.LocalAddr(), target)
		go s.RedirectUDP(pc)

		return nil
	}

	addr, err := s.IPToDomainAddrBuffer(target.IP, make([]byte, utils.MaxAddrLen))
	if err != nil {
		pc := NewUDPConn(conn, target, s)
		s.Add(pc)

		log.Logf("proxy %v <-UDP-> %v", conn.LocalAddr(), target)
		go s.HandleUDP(pc)

		return nil
	}
	binary.BigEndian.PutUint16(addr[len(addr)-2:], uint16(target.Port))

	pc := NewUDPConn(conn, target, s)
	s.Add(pc)

	log.Logf("proxy %v <-UDP-> %v", conn.LocalAddr(), addr)
	go s.HandleUDP(pc)

	return nil
}
