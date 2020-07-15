package netstack

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/miekg/dns"

	"github.com/imgk/shadow/netstack/core"
)

func (s *Stack) HandleUDP(conn PacketConn) error {
	return s.Handler.HandlePacket(conn)
}

func (s *Stack) HandleUDP2(conn core.PacketConn) error {
	defer conn.Close()

	rc, err := net.ListenPacket("udp", "")
	if err != nil {
		return err
	}
	defer rc.Close()

	errCh := make(chan error)
	go Copy2(conn, rc, time.Minute, errCh)

	b := make([]byte, MaxBufferSize)
	for {
		rc.SetReadDeadline(time.Now().Add(time.Minute))
		n, raddr, er := rc.ReadFrom(b)
		if er != nil {
			if ne, ok := er.(net.Error); ok {
				if ne.Timeout() {
					break
				}
			}
			if errors.Is(er, io.EOF) {
				break
			}

			err = er
			break
		}

		if _, er := conn.WriteFrom(b[:n], raddr); er != nil {
			if ne, ok := er.(net.Error); ok {
				if ne.Timeout() {
					break
				}
			}
			if errors.Is(er, io.EOF) {
				break
			}

			err = er
			break
		}
	}

	rc.Close()
	conn.Close()

	if er := <-errCh; er != nil {
		err = er
	}
	return err
}

func Copy2(conn core.PacketConn, rc net.PacketConn, timeout time.Duration, errCh chan error) {
	b := make([]byte, MaxBufferSize)
	for {
		n, addr, err := conn.ReadTo(b)
		if err != nil {
			if ne, ok := err.(net.Error); ok {
				if ne.Timeout() {
					errCh <- nil
					return
				}
			}
			if errors.Is(err, io.EOF) {
				errCh <- nil
				return
			}

			errCh <- err
			return
		}

		if _, err := rc.WriteTo(b[:n], addr); err != nil {
			if ne, ok := err.(net.Error); ok {
				if ne.Timeout() {
					errCh <- nil
					return
				}
			}
			if errors.Is(err, io.EOF) {
				errCh <- nil
				return
			}

			errCh <- err
			return
		}
	}
}

type Message struct {
	b []byte
	m *dns.Msg
}

func NewMessage() Message {
	return Message{
		b: make([]byte, 1024),
		m: new(dns.Msg),
	}
}

func (s *Stack) HandleUDP3(conn core.PacketConn) (err error) {
	defer conn.Close()

	for {
		x := s.Pool.Get().(Message)
		n, addr, er := conn.ReadFrom(x.b[2:])
		if er != nil {
			s.Pool.Put(x)

			if ne, ok := er.(net.Error); ok {
				if ne.Timeout() {
					continue
				}
			}
			if errors.Is(er, io.EOF) {
				return
			}

			err = fmt.Errorf("receive dns error: %w", er)
			return
		}

		go func() {
			defer s.Pool.Put(x)

			if er := x.m.Unpack(x.b[2 : 2+n]); er != nil {
				s.Error(fmt.Sprintf("parse dns error: %v", er))
				return
			}

			if len(x.m.Question) == 0 {
				s.Error("no question")
				return
			}
			s.Info(fmt.Sprintf("%v ask for %v", addr, x.m.Question[0].Name))

			s.HandleMessage(x.m)
			if x.m.MsgHdr.Response {
				bb, er := x.m.PackBuffer(x.b[2:])
				if er != nil {
					s.Error("append message error: " + er.Error())
					return
				}
				n = len(bb)
			} else {
				nr, er := s.Resolver.Resolve(x.b, n)
				if er != nil {
					if ne, ok := er.(net.Error); ok {
						if ne.Timeout() {
							return
						}
					}
					s.Error("resolve dns error: " + er.Error())
					return
				}
				n = nr
			}

			if _, er := conn.WriteTo(x.b[2:2+n], addr); er != nil {
				s.Error("write dns error: " + er.Error())
			}
		}()
	}
	return nil
}
