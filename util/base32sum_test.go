package util

import (
	"testing"
	"bytes"
)

func TestBase32Sum(t *testing.T) {
	prefix := "ABCXYZ!"
	a := []byte("abcdefghijklmnopqrstuvwxyz")
	s := BytesToBase32Sum(a, prefix, 8, true)

	t.Logf("%s\n", s)

	for k := 0; k < 2; k++ {
		for i := 0; i <= 8; i++ {
			b, err := Base32SumToBytes(s[:len(s)-i], prefix, 8-i, (k == 0))
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(a, b) {
				t.Fatalf("a and b not equal")
			}
		}
	}
}

