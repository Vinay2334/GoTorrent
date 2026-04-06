package utils

type BitField []byte

func (bf BitField) HasPiece(index int) bool {
	byteIndex := index / 8
	offset := index % 8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return false
	}
	return bf[byteIndex]&(1<<(7-offset)) != 0
}

func (bf BitField) SetPiece(index int) {
	byteIndex := index / 8
	offset := index % 8
	if byteIndex >= 0 && byteIndex < len(bf) {
		bf[byteIndex] |= 1 << (7 - offset)
	}
}
