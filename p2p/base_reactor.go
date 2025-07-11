package p2p

import (
	"github.com/cometbft/cometbft/v2/libs/service"
	"github.com/cometbft/cometbft/v2/p2p/transport"
)

// Reactor is responsible for handling incoming messages on one or more
// Channel. Switch calls StreamDescriptors when reactor is added to it. When a new
// peer joins our node, InitPeer and AddPeer are called. RemovePeer is called
// when the peer is stopped. Receive is called when a message is received on a
// channel associated with this reactor.
//
// Peer#Send or Peer#TrySend should be used to send the message to a peer.
type Reactor interface {
	service.Service // Start, Stop

	// SetSwitch allows setting a switch.
	SetSwitch(sw *Switch)

	// StreamDescriptors returns the list of stream descriptors. Make sure
	// that each ID is unique across all the reactors added to the switch.
	StreamDescriptors() []transport.StreamDescriptor

	// InitPeer is called by the switch before the peer is started. Use it to
	// initialize data for the peer (e.g. peer state).
	//
	// NOTE: The switch won't call AddPeer nor RemovePeer if it fails to start
	// the peer. Do not store any data associated with the peer in the reactor
	// itself unless you don't want to have a state, which is never cleaned up.
	InitPeer(peer Peer) Peer

	// AddPeer is called by the switch after the peer is added and successfully
	// started. Use it to start goroutines communicating with the peer.
	AddPeer(peer Peer)

	// RemovePeer is called by the switch when the peer is stopped (due to error
	// or other reason).
	RemovePeer(peer Peer, reason any)

	// Receive is called by the switch when an envelope is received from any connected
	// peer on any of the channels registered by the reactor
	Receive(e Envelope)
}

// --------------------------------------

type BaseReactor struct {
	service.BaseService // Provides Start, Stop, .Quit
	Switch              *Switch
}

func NewBaseReactor(name string, impl Reactor) *BaseReactor {
	return &BaseReactor{
		BaseService: *service.NewBaseService(nil, name, impl),
		Switch:      nil,
	}
}

func (br *BaseReactor) SetSwitch(sw *Switch) {
	br.Switch = sw
}
func (*BaseReactor) StreamDescriptors() []transport.StreamDescriptor { return nil }
func (*BaseReactor) AddPeer(Peer)                                    {}
func (*BaseReactor) RemovePeer(Peer, any)                            {}
func (*BaseReactor) Receive(Envelope)                                {}
func (*BaseReactor) InitPeer(peer Peer) Peer                         { return peer }
