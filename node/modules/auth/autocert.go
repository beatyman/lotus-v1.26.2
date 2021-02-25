package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/gwaylib/errors"
)

func CreateTLSCert(certPath, keyPath string) error {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, _ := rand.Int(rand.Reader, max)
	subject := pkix.Name{
		Organization:       []string{"Fivestar"},
		OrganizationalUnit: []string{"lotus"},
		CommonName:         "fivestar-lotus",
	}

	rootTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      subject,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{},
	}
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return errors.As(err)
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &rootTemplate, &rootTemplate, &pk.PublicKey, pk)
	if err != nil {
		return errors.As(err)
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return errors.As(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return errors.As(err)
	}

	certOut.Close()

	keyOut, err := os.Create(keyPath)
	if err != nil {
		return errors.As(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(pk)}); err != nil {
		return errors.As(err)
	}
	keyOut.Close()
	return nil
}
