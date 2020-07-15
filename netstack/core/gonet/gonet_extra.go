package gonet

import (
	"errors"
	"net"

	"gvisor.dev/gvisor/pkg/tcpip/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

type Packet struct {
	Addr net.Addr
	View buffer.View
}

func SendPacket(r *stack.Route, data buffer.VectorisedView, localPort, remotePort uint16) error {
	const ProtocolNumber = udp.ProtocolNumber

	// Allocate a buffer for the UDP header.
	hdr := buffer.NewPrependable(header.UDPMinimumSize + int(r.MaxHeaderLength()))

	// Initialize the header.
	udp := header.UDP(hdr.Prepend(header.UDPMinimumSize))

	length := uint16(hdr.UsedLength() + data.Size())
	udp.Encode(&header.UDPFields{
		SrcPort: localPort,
		DstPort: remotePort,
		Length:  length,
	})

	// Set the checksum field unless TX checksum offload is enabled.
	// On IPv4, UDP checksum is optional, and a zero value indicates the
	// transmitter skipped the checksum generation (RFC768).
	// On IPv6, UDP checksum is not optional (RFC2460 Section 8.1).
	if r.Capabilities()&stack.CapabilityTXChecksumOffload == 0 && r.NetProto == header.IPv6ProtocolNumber {
		xsum := r.PseudoHeaderChecksum(ProtocolNumber, length)
		for _, v := range data.Views() {
			xsum = header.Checksum(v, xsum)
		}
		udp.SetChecksum(^udp.CalculateChecksum(xsum))
	}

	if err := r.WritePacket(nil /* gso */, stack.NetworkHeaderParams{
		Protocol: ProtocolNumber,
		TTL:      r.DefaultTTL(),
		TOS:      0,
	}, &stack.PacketBuffer{
		Header:          hdr,
		Data:            data,
		TransportHeader: buffer.View(udp),
	}); err != nil {
		r.Stats().UDP.PacketSendErrors.Increment()
		return errors.New(err.String())
	}

	// Track count of packets sent.
	r.Stats().UDP.PacketsSent.Increment()
	return nil
}
