package ffiwrapper

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/x509"
	"io"
	"net"

	"github.com/google/uuid"
	"github.com/gwaylib/errors"
)

var (
	_exec_code = uuid.New().String()
)

func readUnixConn(conn net.Conn) ([]byte, error) {
	result := []byte{}
	bufLen := 32 * 1024
	buf := make([]byte, bufLen)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				return nil, errors.As(err)
			}
		}
		result = append(result, buf[:n]...)
		if n < bufLen {
			break
		}
	}
	if len(result) == 0 {
		return nil, io.EOF
	}
	return result, nil
}

func deriveKey(keySize int, password, salt []byte) []byte {
	hash := md5.New()
	out := make([]byte, keySize)
	var digest []byte

	for i := 0; i < len(out); i += len(digest) {
		hash.Reset()
		hash.Write(digest)
		hash.Write(password)
		hash.Write(salt)
		digest = hash.Sum(digest[:0])
		copy(out[i:], digest)
	}
	return out
}

// https://docs.studygolang.com/src/crypto/x509/pem_decrypt.go?s=5819:5931#L186
func AESEncrypt(data []byte, passwd string) (dst []byte, err error) {
	keySize := 32 //AES-256-CBC
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, errors.New("cannot generate IV: " + err.Error())
	}
	key := deriveKey(keySize, []byte(passwd), iv[:8])
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.As(err)
	}
	enc := cipher.NewCBCEncrypter(block, iv)
	pad := aes.BlockSize - len(data)%aes.BlockSize
	encrypted := make([]byte, len(data), len(data)+pad)
	copy(encrypted, data)
	for i := 0; i < pad; i++ {
		encrypted = append(encrypted, byte(pad))
	}
	enc.CryptBlocks(encrypted, encrypted)
	return append(iv, encrypted...), nil
}

// https://docs.studygolang.com/src/crypto/x509/pem_decrypt.go?s=3636:3703#L114
func AESDecrypt(src []byte, passwd string) (dst []byte, err error) {
	keySize := 32 // AES-256-CBC
	iv := src[:aes.BlockSize]
	encrypted := src[aes.BlockSize:]
	key := deriveKey(keySize, []byte(passwd), iv[:8])
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.As(err)
	}
	if len(encrypted)%block.BlockSize() != 0 {
		return nil, errors.New("encrypted PEM data is not a multiple of the block size")
	}
	data := make([]byte, len(encrypted))
	dec := cipher.NewCBCDecrypter(block, iv)
	dec.CryptBlocks(data, encrypted)

	dlen := len(data)
	if dlen == 0 || dlen%aes.BlockSize != 0 {
		return nil, errors.New("invalid padding")
	}
	last := int(data[dlen-1])
	if dlen < last {
		return nil, x509.IncorrectPasswordError
	}
	if last == 0 || last > aes.BlockSize {
		return nil, x509.IncorrectPasswordError
	}
	for _, val := range data[dlen-last:] {
		if int(val) != last {
			return nil, x509.IncorrectPasswordError
		}
	}
	return data[:dlen-last], nil
}
