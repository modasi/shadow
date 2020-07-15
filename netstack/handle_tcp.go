package netstack

import (
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/imgk/shadow/netstack/core"
)

// Max 65536
const MaxBufferSize = 4096

func (s *Stack) HandleTCP(conn net.Conn, addr net.Addr) error {
	return s.Handler.Handle(conn, addr)
}

func (s *Stack) HandleTCP2(conn net.Conn, addr net.Addr) error {
	defer conn.Close()

	rc, err := net.DialTCP("tcp", nil, addr.(*net.TCPAddr))
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := Relay(conn, rc); err != nil {
		if ne, ok := err.(net.Error); ok {
			if ne.Timeout() {
				return nil
			}
		}
		if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, io.EOF) {
			return nil
		}

		return fmt.Errorf("relay error: %w", err)
	}
	return nil
}

type CloseReader interface {
	CloseRead() error
}

type CloseWriter interface {
	CloseWrite() error
}

type DuplexConn interface {
	net.Conn
	CloseReader
	CloseWriter
}

type Conn struct {
	net.Conn
}

func (c Conn) ReadFrom(r io.Reader) (int64, error) {
	return Copy(c.Conn, r)
}

func (c Conn) WriteTo(w io.Writer) (int64, error) {
	return Copy(w, c.Conn)
}

func (c Conn) CloseRead() error {
	if close, ok := c.Conn.(CloseReader); ok {
		return close.CloseRead()
	}

	return c.Conn.Close()
}

func (c Conn) CloseWrite() error {
	if close, ok := c.Conn.(CloseWriter); ok {
		return close.CloseWrite()
	}

	return c.Conn.Close()
}

func Relay(c, rc net.Conn) error {
	l, ok := c.(core.Conn)
	if !ok {
		return fmt.Errorf("relay error")
	}

	r, ok := rc.(DuplexConn)
	if !ok {
		r = Conn{Conn: rc}
	}

	return relay(l, r)
}

func relay(c core.Conn, rc DuplexConn) error {
	errCh := make(chan error, 1)
	go relay2(c, rc, errCh)

	_, err := Copy(c, rc)
	if err != nil {
		c.Close()
		rc.Close()
	} else {
		c.CloseWrite()
		rc.CloseRead()
	}

	if err != nil {
		<-errCh
		return err
	}

	return <-errCh
}

func relay2(c core.Conn, rc DuplexConn, errCh chan error) {
	_, err := Copy(rc, c)
	if err != nil {
		rc.Close()
		c.Close()
	} else {
		rc.CloseWrite()
		c.CloseRead()
	}

	errCh <- err
}

func Copy(w io.Writer, r io.Reader) (n int64, err error) {
	if wt, ok := r.(io.WriterTo); ok {
		return wt.WriteTo(w)
	}
	if rt, ok := w.(io.ReaderFrom); ok {
		return rt.ReadFrom(r)
	}

	b := make([]byte, MaxBufferSize)
	for {
		nr, er := r.Read(b)
		if nr > 0 {
			nw, ew := w.Write(b[:nr])
			if nw > 0 {
				n += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}

	return n, err
}
