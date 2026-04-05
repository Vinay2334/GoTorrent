package main

import (
	"fmt"
	"io"
	"net"
	"time"
)

type Handshake struct {
	Pstr     string
	infoHash [20]byte
	peerID   [20]byte
}

func NewHandshake(infoHash [20]byte, peerID [20]byte) *Handshake {
	return &Handshake{
		Pstr:     "BitTorrent protocol",
		infoHash: infoHash,
		peerID:   peerID,
	}
}

func (h *Handshake) Serialize() []byte {
	buf := make([]byte, 49+len(h.Pstr))
	buf[0] = byte(len(h.Pstr))
	idx := 1
	idx += copy(buf[idx:], []byte(h.Pstr))
	idx += copy(buf[idx:], make([]byte, 8)) // reserved bytes
	idx += copy(buf[idx:], h.infoHash[:])
	copy(buf[idx:], h.peerID[:])
	return buf
}

func StartPeerHandshake(addr string, infoHash [20]byte, peerID [20]byte) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to peer: %v", err)
	}
	defer conn.Close()
	hs := NewHandshake(infoHash, peerID)
	_, err = conn.Write(hs.Serialize())
	if err != nil {
		return fmt.Errorf("failed to send handshake: %v", err)
	}

	res := make([]byte, 68)
	_, err = io.ReadFull(conn, res)
	if err != nil {
		return fmt.Errorf("failed to read handshake response: %v", err)
	}

	var remoteInfoHash [20]byte
	copy(remoteInfoHash[:], res[28:48])
	if remoteInfoHash != infoHash {
		return fmt.Errorf("info hash mismatch in handshake response")
	}

	fmt.Printf("Handshake successfull with peerId: %s \n", addr)
	return nil
}
