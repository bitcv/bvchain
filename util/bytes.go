package util


func IsZeros(bz []byte) bool {
	for _, b := range bz {
		if b != byte(0) {
			return false
		}
	}
	return true
}

func RightPadBytes(bz []byte, l int) []byte {
	if l < len(bz) {
		return bz
	}
	padded := make([]byte, l)
	copy(padded[0:len(bz)], bz)
	return padded
}

func LeftPadBytes(bz []byte, l int) []byte {
	if l < len(bz) {
		return bz
	}
	padded := make([]byte, l)
	copy(padded[l-len(bz):], bz)
	return padded
}

func CloneBytes(bz []byte) []byte {
	bz2 := make([]byte, len(bz))
	copy(bz2, bz)
	return bz2
}

