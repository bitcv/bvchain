package ec

import (
	"testing"
	"fmt"
	"bytes"
	"encoding/hex"
)


func TestAddress(t *testing.T) {
	b, _ := hex.DecodeString("9C256576D674DF7496EA81D3383B4813D692C6E0")
//	b, _ := hex.DecodeString("8E495C1D6089DAD15DC7D7B46154D6B6CE8EF4AD")
	a1 := BytesToAddress(b)
	s := a1.String()
	a2, err := StringToAddress(s)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(s)
	if !bytes.Equal(a1[:], a2[:]) {
		fmt.Println(a1)
		fmt.Println(a2)
		t.Fatalf("addresses not equal")
	}
}
