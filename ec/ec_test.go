package ec

import (
	"testing"
	"bytes"
)

func TestPrivKeys(t *testing.T) {
	msg := []byte("hello,world!")
	k := NewPrivKey()
	p := k.PubKey()
	a := p.Address()
	sig, err := k.SignMessage(msg)
	if err != nil {
		t.Fatalf("(PrivKey).Sign() failed")
	}

	ok := sig.VerifyMessagePubKey(msg, p)
	if !ok {
		t.Fatalf("(*Signature).VerifyPubKey() failed")
	}

	t.Logf("k %x\n\t\t%s\n\n", k[:], k)
	t.Logf("p %x\n\t\t%s\n\n", p[:], p)
	t.Logf("a %x\n\t\t%s\n\n", a[:], a)
	t.Logf("s %x %x %x\n\t\t%s\n\n", sig[:1], sig[1:33], sig[33:], sig)
}

func TestSharedSecret(t *testing.T) {
	k1 := NewPrivKey()
	k2 := NewPrivKey()

	p1 := k1.PubKey()
	p2 := k2.PubKey()

	s1, e1 := k1.SharedSecret(p2)
	s2, e2 := k2.SharedSecret(p1)

	if e1 != nil || e2 != nil || !bytes.Equal(s1, s2) {
		t.Logf("secret %x\n", s1)
		t.Logf("secret %x\n", s2)
		t.Fatalf("Failed to compute shared secret")
	}
}

