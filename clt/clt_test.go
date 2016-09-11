package clt

import "testing"

func TestKeyReading(t *testing.T) {
	k, err := readLocalKey()
	if err != nil {
		t.Fatal(err.Error())
	}
	if k == nil {
		t.Fatal("key is not supposed to be nil")
	}
	if len(k.Pub) == 0 {
		t.Fatal("public key is not read")
	}
	if len(k.Priv) == 0 {
		t.Fatal("public key is not read")
	}
}
