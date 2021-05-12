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

	encodeData, err := EncodeWallet([]byte("hello"), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if originData, err := DecodeWallet(encodeData, "abc"); err != nil {
		t.Fatal(err)
	} else if string(originData) != "hello" {
		t.Fatal(string(originData))
	}

}

func TestRSARootHash(t *testing.T) {
	fmt.Println(RootKeyHash())
}
