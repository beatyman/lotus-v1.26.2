package auth

import (
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"testing"
)

func TestGenRootCert(t *testing.T) {
	priv, err := GenRsaKey()
	if err != nil {
		t.Fatal(err)
	}
	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	//fmt.Printf("%s\n", hex.EncodeToString(privBytes))

	// reset the root cert
	//rootPriv = priv

	// testing encript
	data := make([]byte, 4096*10)
	if _, err := RSAEncript(data, priv.Public().(*rsa.PublicKey)); err != nil {
		t.Fatal(err)
	}

	// x509 testing
	if _, err := x509.ParsePKCS1PrivateKey(privBytes); err != nil {
		t.Fatal(err)
	}

	encodeData, err := EncodeRSAKey(priv, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRSAKey(encodeData, "abc"); err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(encodeData))
}

func TestRSARootHash(t *testing.T) {
	fmt.Println(RootKeyHash())
}
