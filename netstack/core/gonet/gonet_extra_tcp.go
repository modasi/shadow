package gonet

import (
	"io"
)

func (c *TCPConn) commonRead() (int, []byte, error) {
	bb, err := commonRead(c.ep, c.wq, c.readCancel(), nil, c, false)
	nr := len(bb)
	return nr, bb, err
}

// WriteTo implements io.WriterTo.WriteTo.
func (c *TCPConn) WriteTo(w io.Writer) (n int64, err error) {
	c.readMu.Lock()
	for {
		nr, bb, er := c.commonRead()
		if nr > 0 {
			nw, ew := w.Write(bb)
			if nw > 0 {
				n += int64(nw)
				c.ep.ModerateRecvBuf(nw)
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
				break
			}
			break
		}
	}
	c.readMu.Unlock()
	return
}
