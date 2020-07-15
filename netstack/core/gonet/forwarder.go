package gonet

import (
	"errors"
	"unsafe"

	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

type ForwarderRequest struct {
	Stack *stack.Stack
	Route *stack.Route
	ID    stack.TransportEndpointID
	Pkt   *stack.PacketBuffer
}

func (rr *ForwarderRequest) CreateUDPConn() (*UDPConn, error) {
	r := (*udp.ForwarderRequest)(unsafe.Pointer(rr))

	wq := waiter.Queue{}
	ep, tcperr := r.CreateEndpoint(&wq)
	if tcperr != nil {
		return nil, errors.New(tcperr.String())
	}

	return NewUDPConn(rr.Stack, &wq, ep), nil
}

func (rr *ForwarderRequest) CreateUDPConn2() (*UDPConn2, error) {
	conn := NewUDPConn2(rr)
	if err := conn.HandleRequest(rr); err != nil {
		return nil, err
	}
	conn.HandlePacket(rr.Route, rr.ID, rr.Pkt)
	return conn, nil
}

func (rr *ForwarderRequest) CreateUDPConn3() (*UDPConn3, error) {
	conn := NewUDPConn3(rr)
	if err := conn.HandleRequest(rr); err != nil {
		return nil, err
	}
	conn.HandlePacket(rr.Route, rr.ID, rr.Pkt)
	return conn, nil
}
