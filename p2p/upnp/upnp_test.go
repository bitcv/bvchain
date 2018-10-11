package upnp

import (
	"testing"
)

func TestUpnp(t *testing.T) {
	nat, err := Discover()
	if err != nil {
		t.Errorf("%s\n", err)
	}

	if nat != nil {
		addr, err := nat.GetExternalAddress()
		t.Logf("ext-addr: %v\n", addr)
		if err != nil {
			t.Errorf("%s\n", err)
		}
	}
}

