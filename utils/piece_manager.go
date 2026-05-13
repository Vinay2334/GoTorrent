package utils

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"sync"
)

type PieceStatus int

const (
	PieceNotStarted PieceStatus = iota
	PieceDownloading
	PieceFinished
)

type PieceManager struct {
	TotalPieces int64
	PieceLength int64
	TotalLength int64

	MyBitfield    BitField
	Statuses      []PieceStatus
	Mu            sync.Mutex
	Buffers       map[int][]byte
	DownloadedAmt map[int]int64
	Hashes        [][20]byte
}

func NewPieceManager(totalLength, pieceLength int64, hashes [][20]byte) *PieceManager {
	numPieces := (totalLength + pieceLength - 1) / pieceLength
	return &PieceManager{
		TotalPieces:   numPieces,
		PieceLength:   pieceLength,
		TotalLength:   totalLength,
		MyBitfield:    make(BitField, (numPieces+7)/8), // +7 to round up to full bytes
		Statuses:      make([]PieceStatus, numPieces),
		Buffers:       make(map[int][]byte),
		DownloadedAmt: make(map[int]int64),
		Hashes:        hashes}
}

func (pm *PieceManager) PickPiece(peerBf BitField) (int, bool) {
	pm.Mu.Lock()
	defer pm.Mu.Unlock()

	for i := 0; i < int(pm.TotalPieces); i++ {
		if pm.Statuses[i] == PieceNotStarted && peerBf.HasPiece(i) {
			pm.Statuses[i] = PieceDownloading
			return i, true
		}
	}
	return 0, false
}

func (pm *PieceManager) AddBlock(index int, offset int, block []byte) ([]byte, bool) {
	pm.Mu.Lock()
	defer pm.Mu.Unlock()

	if _, exists := pm.Buffers[index]; !exists {
		pm.Buffers[index] = make([]byte, pm.GetPieceLength(index))
	}

	copy(pm.Buffers[index][offset:], block)
	pm.DownloadedAmt[index] += int64(len(block))

	if pm.DownloadedAmt[index] == pm.GetPieceLength(index) {
		data := pm.Buffers[index]

		hash := sha1.Sum(data)
		if !bytes.Equal(hash[:], pm.Hashes[index][:]) {
			fmt.Printf("Hash mismatch for piece %d: expected %x, got %x\n", index, pm.Hashes[index], hash)
			return nil, false
		}

		delete(pm.Buffers, index)
		delete(pm.DownloadedAmt, index)
		return data, true
	}

	return nil, false
}

func (pm *PieceManager) GetPieceLength(index int) int64 {
	if int64(index) == pm.TotalPieces-1 {
		return pm.TotalLength - (int64(index) * pm.PieceLength)
	}
	return pm.PieceLength
}

func (pm *PieceManager) MarkPieceFinished(index int) {
	pm.Mu.Lock()
	defer pm.Mu.Unlock()
	pm.Statuses[index] = PieceFinished
	pm.MyBitfield.SetPiece(index)
}

func (pm *PieceManager) BuildBitField(fm *FileManager) {
	for i := 0; i < int(pm.TotalPieces); i++ {
		data, err := fm.ReadPiece(i, pm.PieceLength, 0)
		if err != nil {
			continue
		}
		hash := sha1.Sum(data)
		if bytes.Equal(hash[:], pm.Hashes[i][:]) {
			pm.MyBitfield.SetPiece(i)
			pm.Statuses[i] = PieceFinished
		} else {
			fmt.Printf("Piece %d hash failed.\n", i)
		}
	}
}
