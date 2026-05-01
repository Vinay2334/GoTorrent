package main

import (
	"Torrent/utils"
	"encoding/binary"
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
)

type Message struct {
	ID      MessageID
	Payload []byte
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

func SendInterested(w io.Writer) error {
	msg := []byte{0, 0, 0, 1, byte(MsgInterested)}
	_, err := w.Write(msg)
	return err
}

func ReadMessage(r io.Reader) (*Message, error) {
	lengthBuf := make([]byte, 4)
	_, err := io.ReadFull(r, lengthBuf)
	if err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(lengthBuf)
	if length == 0 {
		return nil, nil
	}

	msgBuf := make([]byte, length)
	_, err = io.ReadFull(r, msgBuf)
	if err != nil {
		return nil, err
	}
	return &Message{ID: MessageID(msgBuf[0]), Payload: msgBuf[1:]}, nil
}

func handlePeerMessages(conn net.Conn, pm *utils.PieceManager, fm *utils.FileManager) {
	defer conn.Close()
	choked := true
	var peerBitField utils.BitField

	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C: // Send keep-alive every 2 minutes
			fmt.Printf("Sending keep-alive to peer %s\n", conn.RemoteAddr())
			_, err := conn.Write([]byte{0, 0, 0, 0})
			if err != nil {
				fmt.Printf("Error sending keep-alive: %v\n", err)
				return
			}
		default:
			msg, err := ReadMessage(conn)
			if err != nil {
				fmt.Printf("Error reading message from peer: %v\n", err)
				return
			}

			if msg == nil {
				continue
			}

			switch msg.ID {
			case MsgChoke:
				choked = true
				fmt.Println("Peer choked us")
			case MsgUnchoke:
				index, found := pm.PickPiece(peerBitField)
				if found {
					fmt.Printf("Peer unchoked us, requesting piece index %d\n", index)
					for offset := int64(0); offset < pm.PieceLength; offset += 16384 {
						blockSize := int64(16384)
						if offset+blockSize > pm.PieceLength {
							blockSize = pm.PieceLength - offset
						}

						request := make([]byte, 17)
						binary.BigEndian.PutUint32(request[0:4], 13)
						request[4] = byte(MsgRequest)
						binary.BigEndian.PutUint32(request[5:9], uint32(index))
						binary.BigEndian.PutUint32(request[9:13], uint32(offset))
						binary.BigEndian.PutUint32(request[13:17], uint32(blockSize))

						conn.Write(request)
					}
				}
			case MsgHave:
				index := int(binary.BigEndian.Uint32(msg.Payload))
				peerBitField.SetPiece(index)
			case MsgBitfield:
				peerBitField = utils.BitField(msg.Payload)
				fmt.Printf("Received bitfield (length %d)\n", len(peerBitField))
			case MsgPiece:
				if len(msg.Payload) < 8 {
					fmt.Println("Payload too short for piece message")
					continue
				}

				index := binary.BigEndian.Uint32(msg.Payload[0:4])
				begin := binary.BigEndian.Uint32(msg.Payload[4:8])
				block := msg.Payload[8:]
				fmt.Printf("Received piece index %d, begin %d, block length %d\n", index, begin, len(block))

				data, complete := pm.AddBlock(int(index), int(begin), block)
				if complete {
					fmt.Printf("Completed piece index %d\n", index)
					err := fm.WritePiece(int(index), pm.PieceLength, data)
					if err != nil {
						fmt.Printf("Error writing piece to disk: %v\n", err)
					}
					pm.MarkPieceFinished(int(index))
					fmt.Printf("My bitfield after completing piece %d: %v\n", index, pm.MyBitfield)
				}
			}
			_ = choked
		}
	}
}

func StartPeerHandshake(addr string, infoHash [20]byte, peerID [20]byte, pm *utils.PieceManager, fm *utils.FileManager) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to peer: %v", err)
	}

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

	err = SendInterested(conn)
	if err != nil {
		return fmt.Errorf("failed to send interested message: %v", err)
	}

	handlePeerMessages(conn, pm, fm)

	fmt.Printf("Handshake successfull with peerId: %s \n", addr)
	return nil
}
