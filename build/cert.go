package build

import (
	"crypto/md5"
	"crypto/rsa"
	"crypto/x509"
	"embed"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/gwaylib/errors"
)

var (
	certfs      embed.FS
	oldRootPriv *rsa.PrivateKey // nil for not exists.
)

type RootCert struct {
	Hash       string
	PrivateKey *rsa.PrivateKey
}

func GetRootCert() (*RootCert, *RootCert) {
	rootData, err := certfs.ReadFile(path.Join("cert", "root.key"))
	if err != nil {
		panic(errors.As(err))
	}
	rootPrivKey, err := hex.DecodeString(strings.TrimSpace(string(rootData)))
	if err != nil {
		panic(errors.As(err))
	}
	rootPriv, err := x509.ParsePKCS1PrivateKey(rootPrivKey)
	if err != nil {
		panic(errors.As(err))
	}
	root := &RootCert{
		Hash:       fmt.Sprintf("%x", md5.Sum(rootPrivKey)),
		PrivateKey: rootPriv,
	}

	oldKey, err := certfs.ReadFile(path.Join("cert", "root-old.key"))
	if err != nil {
		// ignore
		return root, nil
	}

	oldRootPrivKey, err := hex.DecodeString(strings.TrimSpace(string(oldKey)))
	if err != nil {
		log.Warn(errors.As(err, "root-old.key"))
		return root, nil
	}
	oldPriv, err := x509.ParsePKCS1PrivateKey(oldRootPrivKey)
	if err != nil {
		log.Warn(err, "root-old.key")
		return root, nil
	}
	return root, &RootCert{
		Hash:       fmt.Sprintf("%x", md5.Sum(oldRootPrivKey)),
		PrivateKey: oldPriv,
	}
}
