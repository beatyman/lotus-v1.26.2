package auth

import (
	"crypto/x509"
	"encoding/hex"
	"strings"

	rice "github.com/GeertJohan/go.rice"
)

var (
	// TODO: 发版时需要编译时写入, 以便可以各自指定
	rootPrivKey, _ = hex.DecodeString(strings.TrimSpace(rice.MustFindBox("../../../build/bootstrap").MustString("root.key")))
	rootPriv, _    = x509.ParsePKCS1PrivateKey(rootPrivKey)
)
