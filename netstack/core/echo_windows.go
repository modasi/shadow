// +build ignore

package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"

	"github.com/imgk/shadow/device/windivert"
	"github.com/imgk/shadow/netstack/core"
)

type Handler struct{}

func (h Handler) Handle(conn core.Conn) {
	defer conn.Close()

	fmt.Printf("%v <-TCP-> %v\n", conn.RemoteAddr(), conn.LocalAddr())
	bb := make([]byte, len(b))
	_, err := io.ReadFull(conn, bb)
	if err != nil {
		panic(err)
	}
	if _, err := conn.Write(bb); err != nil {
		panic(err)
	}
}

func (h Handler) HandlePacket(conn core.PacketConn) {
	defer conn.Close()

	fmt.Printf("%v <-UDP-> %v\n", conn.RemoteAddr(), conn.LocalAddr())
	bb := make([]byte, len(b))
	_, err := io.ReadFull(conn, bb)
	if err != nil {
		panic(err)
	}
	if _, err := conn.Write(bb); err != nil {
		panic(err)
	}
}

func main() {
	dev, err := windivert.NewDevice("outbound and ip.DstAddr == 8.8.8.8")
	if err != nil {
		panic(fmt.Errorf("windivert error: %w", err))
	}
	defer dev.Close()
	dev.IPFilter.Add("0.0.0.0/1")
	dev.IPFilter.Add("128.0.0.0/1")

	stack := core.NewStack(dev, Handler{})

	go func() {
		if _, err := dev.WriteTo(stack); err != nil {
			panic(fmt.Errorf("netstack exit error: %v", err))
		}
	}()

	TestTCP()
	TestUDP()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, os.Kill)
	<-sigCh
}

var b []byte = []byte("The http package's Transport and Server both automatically enable HTTP/2 support for simple configurations.")

func TestTCP() {
	conn, err := net.Dial("tcp", "8.8.8.8:1122")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if _, err := conn.Write(b); err != nil {
		panic(err)
	}

	bb := make([]byte, len(b))
	if _, err := io.ReadFull(conn, bb); err != nil {
		panic(err)
	}

	fmt.Println("Read TCP", string(bb))
}

func TestUDP() {
	conn, err := net.Dial("tcp", "8.8.8.8:1122")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if _, err := conn.Write(b); err != nil {
		panic(err)
	}

	bb := make([]byte, len(b))
	if _, err := io.ReadFull(conn, bb); err != nil {
		panic(err)
	}

	fmt.Println("Read UDP", string(bb))
}
