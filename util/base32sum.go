package util

import (
	"fmt"
	"strings"
	"halftwo/mangos/crock32"
	"golang.org/x/crypto/sha3"
)

func BytesToBase32Sum(src []byte, prefix string, suffix int, caseSensitive bool) string {
	var hash []byte
	if suffix > 0 || caseSensitive {
		h := sha3.New256()
		h.Write([]byte(prefix))
		h.Write(src)
		hash = h.Sum(nil)
	}

	if suffix > 8 {
		suffix = 8
	} else if suffix < 0 {
		suffix = 0
	}

	plen := len(prefix)
	b32len := crock32.EncodeLen(len(src))
	buf := make([]byte, plen + b32len + suffix)
	copy(buf[:], prefix)
	crock32.EncodeLower(buf[plen:], src)

	if suffix > 0 {
		crock32.EncodeLower(buf[plen+b32len:], hash)
	}

	if caseSensitive {
		dst := buf[plen:]
		hash = hash[5:]
		for {
			num := len(dst)
			if num == 0 || len(hash) == 0 {
				break
			}

			if num > 8 {
				num = 8
			}

			bitmap := hash[0]
			mask := byte(0x80)
			for k := 0; k < num; k++ {
				if (mask & bitmap) != 0 {
					if dst[k] >= 'a' && dst[k] <= 'z' {
						dst[k] -= 32
					}
				}
				mask >>= 1
			}

			dst = dst[num:]
			hash = hash[1:]
		}
	}
	return string(buf)
}

func Base32SumToBytes(str string, prefix string, suffix int, caseSensitive bool) (b []byte, err error) {
	if !strings.HasPrefix(str, prefix) {
		return nil, fmt.Errorf("the string must begin with \"%s\"", prefix)
	}

	if suffix > 8 {
		suffix = 8
	} else if suffix < 0 {
		suffix = 0
	}

	b32len := len(str) - len(prefix) - suffix
	if b32len <= 1 {
		return nil, fmt.Errorf("the string is too short")
	}

	b, err = crock32.DecodeString(str[len(prefix):len(str)-suffix])
	if err != nil {
		return
	}

	if suffix > 0 || caseSensitive {
		t := BytesToBase32Sum(b, prefix, suffix, caseSensitive)
		var equal bool
		if caseSensitive {
			equal = (t == str)
		} else {
			equal = strings.EqualFold(t, str)
		}

		if !equal {
			return nil, fmt.Errorf("invalid checksum")
		}
	}
	return
}

