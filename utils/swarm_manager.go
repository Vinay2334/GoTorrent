package utils

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

type MessageID uint8

const (
	MsgChoke MessageID = iota
	MsgUnchoke
	MsgInterested
	MsgNotInterested
	MsgHave
	MsgBitfield
	MsgRequest
	MsgPiece
	MsgExtended MessageID = 20
)

type PeerState struct {
	Addr     string   // IP:Port
	Conn     net.Conn // The actual TCP socket
	BitField BitField // What this specific peer has (updated by Bitfield/Have)

	// State Machine Flags
	AmChoking      bool // Are we choking them? (Initially true)
	AmInterested   bool // Are we interested in them? (Initially false)
	PeerChoking    bool // Are they choking us? (Initially true)
	PeerInterested bool // Are they interested in us? (Initially false)

	// Extensions & Metadata
	ExtensionIDs map[string]int // IDs for ut_pex, ut_metadata, etc.
	SupportsPEX  bool           // Derived from handshake/extensions

	// Performance Tracking (Essential for Choking Algorithms)
	DownloadSpeed float64 // Bytes per second (for picking who to unchoke)
	UploadSpeed   float64
	LastMessage   time.Time // To handle keep-alives and timeouts
}

type Swarm struct {
	InfoHash [20]byte
	PeerID   [20]byte
	Peers    map[string]*PeerState
	mu       sync.RWMutex
}

func NewSwarm(infoHash [20]byte, peerID [20]byte) *Swarm {
	return &Swarm{
		InfoHash: infoHash,
		PeerID:   peerID,
		Peers:    make(map[string]*PeerState),
	}
}

func (s *Swarm) AddPeer(addr string, conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Peers[addr] = &PeerState{Addr: addr, Conn: conn, AmInterested: false, AmChoking: true}
}

func (s *Swarm) RemovePeer(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Peers, addr)
}

func (s *Swarm) SetMsgState(addr string, msgType MessageID, value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, exists := s.Peers[addr]
	if !exists {
		return
	}
	switch msgType {
	case MsgChoke:
		peer.AmChoking = value
	case MsgUnchoke:
		peer.AmChoking = value
	case MsgInterested:
		peer.AmInterested = value
	case MsgNotInterested:
		peer.AmInterested = value
	}
}

func (s *Swarm) CountUnchoked() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, peer := range s.Peers {
		if !peer.AmChoking {
			count++
		}
	}
	return count
}

func (s *Swarm) BroadcastHave(index int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 5 bytes: 1 byte ID (MsgHave = 4) + 4 bytes Index
	msg := make([]byte, 9)
	binary.BigEndian.PutUint32(msg[0:4], 5)
	msg[4] = 4 // MsgHave ID
	binary.BigEndian.PutUint32(msg[5:9], uint32(index))

	for addr, peer := range s.Peers {
		_, err := peer.Conn.Write(msg)
		if err != nil {
			fmt.Printf("Failed to send Have to %s: %v\n", addr, err)
		}
	}
}
