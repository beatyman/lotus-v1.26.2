package encode

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"testing"
)

func TestCrypto(t *testing.T) {
	priv, err := GenRsaKey()
	if err != nil {
		t.Fatal(err)
	}
	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	// x509 testing
	if _, err := x509.ParsePKCS1PrivateKey(privBytes); err != nil {
		t.Fatal(err)
	}

	//fmt.Printf("%s\n", hex.EncodeToString(privBytes))

	// reset the root cert
	//rootPriv = priv

	data := make([]byte, 4096*10)

	// test rsa
	rsaData, err := RSAEncript(data, priv.Public().(*rsa.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	oData, err := RSADecript(rsaData, priv)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(data, oData) != 0 {
		t.Fatal("rsa test failed")
	}

	// test aes
	aesData, err := AESEncript(data, "abc")
	if err != nil {
		t.Fatal(err)
	}
	oData, err = AESDecript(aesData, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(data, oData) != 0 {
		t.Fatal("aes test failed")
	}

	encodeData, err := MixEncript([]byte("hello"), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if originData, err := MixDecript(encodeData, "abc"); err != nil {
		t.Fatal(err)
	} else if string(originData) != "hello" {
		t.Fatal(string(originData))
	}
}

func TestRSARootHash(t *testing.T) {
	fmt.Println(RootKeyHash())
}
