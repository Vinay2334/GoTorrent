package utils

import (
	"sync"
)

type PieceStatus int

const (
	PieceNotStarted PieceStatus = iota
	PieceDownloading
	PieceFinished
)

type PieceManager struct {
	TotalPieces int
	PieceLength int
	TotalLength int

	MyBitfield BitField
	Statuses   []PieceStatus
	Mu         sync.Mutex
}

func NewPieceManager(totalLength, pieceLength int) *PieceManager {
	numPieces := (totalLength + pieceLength - 1) / pieceLength
	return &PieceManager{
		TotalPieces: numPieces,
		PieceLength: pieceLength,
		TotalLength: totalLength,
		MyBitfield:  make(BitField, (numPieces+7)/8),
		Statuses:    make([]PieceStatus, numPieces),
	}
}

func (pm *PieceManager) PickPiece(peerBf BitField) (int, bool) {
	pm.Mu.Lock()
	defer pm.Mu.Unlock()

	for i := 0; i < pm.TotalPieces; i++ {
		if pm.Statuses[i] == PieceNotStarted && peerBf.HasPiece(i) {
			pm.Statuses[i] = PieceDownloading
			return i, true
		}
	}
	return 0, false
}
