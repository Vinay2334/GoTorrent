package main

import (
	"Torrent/bencode"
	"Torrent/utils"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"reflect"
	"time"
)

type Handshake struct {
	Pstr     string
	infoHash [20]byte
	peerID   [20]byte
}

type ExtensionHandshake struct {
	M    map[string]any `bencode:"m"`
	V    string         `bencode:"v"`
	ReqQ any            `bencode:"reqq"`
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
	MsgExtended MessageID = 20
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

	reserved := [8]byte{}
	reserved[5] = 0x10 // Set the extension protocol bit (BEP 10) in the reserved bytes
	idx += copy(buf[idx:], reserved[:])

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

// Client support PEX Protocol
func pexMessage() []byte {
	extHandshake := map[string]interface{}{
		"m": map[string]interface{}{"ut_pex": 1},
	}
	encoded, _ := bencode.Encode(extHandshake)
	extMsg := make([]byte, 6+len(encoded))
	binary.BigEndian.PutUint32(extMsg[0:4], uint32(2+len(encoded)))
	extMsg[4] = byte(MsgExtended)
	extMsg[5] = 0 // 0 is the ID for the extension handshake itself
	copy(extMsg[6:], encoded)
	return extMsg
}

func handlePeerMessages(conn net.Conn, pm *utils.PieceManager, fm *utils.FileManager, peerChan chan<- []string) {
	defer conn.Close()
	choked := true
	var peerBitField utils.BitField

	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	var pexId, metadataId int64

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
			case MsgExtended:
				extendedID := msg.Payload[0]
				pexPayload := msg.Payload[1:]

				if extendedID == 0 {
					fmt.Println("Received extension handshake from peer")
					var handshake ExtensionHandshake
					data := bytes.NewReader(pexPayload)
					extHandshake, err := bencode.Decode(data)
					if err != nil {
						fmt.Printf("Error decoding extension handshake: %v\n", err)
					}
					fmt.Printf("Extension handshake data: %v\n", extHandshake)
					unmarshal(extHandshake, &handshake)
					fmt.Printf("Parsed extension handshake: %+v\n", handshake)
					if pexID, ok := handshake.M["ut_pex"]; ok {
						fmt.Printf("Peer supports PEX with ID: %d\n", pexID)
						pexId = pexID.(int64)
					}
					// TODO: Handle metadata exchange if peer supports it
					if metadataID, ok := handshake.M["ut_metadata"]; ok {
						metadataId = metadataID.(int64)
						fmt.Printf("Peer supports Metadata with ID: %d\n", metadataId)
						// err := sendMetadataRequest(conn, metadataId, 0)
						// if err != nil {
						// 	fmt.Printf("Error handling metadata message: %v\n", err)
						// 	continue
						// }
					}
				} else if extendedID == byte(pexId) {
					fmt.Println("Received PEX message from peer")
					pexData, err := bencode.Decode(bytes.NewReader(pexPayload))
					if err != nil {
						fmt.Printf("Error decoding PEX message: %v\n", err)
						continue
					}

					if added, ok := pexData["added"].(string); ok {
						newPeers := parseCompactPeers(added)
						for _, addr := range newPeers {
							fmt.Printf("Received new peer from PEX: %s\n", addr)
							peerChan <- []string{addr}
						}
					}
				}
			}
			_ = choked
		}
	}
}

func StartPeerHandshake(addr string, infoHash [20]byte, peerID [20]byte, pm *utils.PieceManager, fm *utils.FileManager, peerChan chan<- []string) error {
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

	if res[25]&0x10 == 0 {
		fmt.Println("Peer does not support extensions")
	}
	_, err = conn.Write(pexMessage())
	if err != nil {
		return err
	}

	bfMsg := append([]byte{byte(MsgBitfield)}, pm.MyBitfield...)
	bfLen := make([]byte, 4)
	binary.BigEndian.PutUint32(bfLen, uint32(len(bfMsg)))
	conn.Write(append(bfLen, bfMsg...))

	err = SendInterested(conn)
	if err != nil {
		return fmt.Errorf("failed to send interested message: %v", err)
	}

	handlePeerMessages(conn, pm, fm, peerChan)

	fmt.Printf("Handshake successfull with peerId: %s \n", addr)
	return nil
}

func parseCompactPeers(peersBin string) []string {
	const peerSize = 6
	numPeers := len(peersBin) / peerSize
	peers := make([]string, numPeers)

	for i := 0; i < numPeers; i++ {
		offset := i * peerSize
		ip := net.IP([]byte(peersBin[offset : offset+4]))
		port := binary.BigEndian.Uint16([]byte(peersBin[offset+4 : offset+6]))
		peers[i] = fmt.Sprintf("%s:%d", ip.String(), port)
	}
	return peers
}

func sendMetadataRequest(conn net.Conn, metadataID int, piece int) error {
	payload := map[string]interface{}{
		"msg_type": 0,
		"piece":    piece,
	}
	encoded, _ := bencode.Encode(payload)

	// Length: 1 (Extension ID) + 1 (Metadata ID) + len(encoded)
	msgLen := uint32(2 + len(encoded))
	buf := make([]byte, 4+msgLen)

	binary.BigEndian.PutUint32(buf[0:4], msgLen)
	buf[4] = byte(MsgExtended)
	buf[5] = byte(metadataID)
	copy(buf[6:], encoded)

	_, err := conn.Write(buf)
	return err
}

func unmarshal(data map[string]any, v any) {
	rv := reflect.ValueOf(v).Elem()
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("bencode")

		if val, ok := data[tag]; ok {
			rv.Field(i).Set(reflect.ValueOf(val))
		}
	}
}
