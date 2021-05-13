// 生成过程
// 一，编译
// 1, 独立制作程序根证书, 根证书一般由编译人员掌握
// 2, 编译出程序
// 3, 当根证书变更时，必须对所有钱包密钥变更才能运行, 因此正常情况下根证书不需变更。
//
// 二，加密钱包
// 1, 使用前面编译的程序，独立生成加密指令
// 2, 内存创建原始钱包密钥(三个地址，o_wallet)
// 3, 加密机使用AES256加密o_wallet, 得到e1_wallet
// 5, 通过根证书加密e1_wallet，得到e2_wallet
// 7, 导出e2_wallet得到e2_wallet部署文件(三个文件，对应三个钱包地址)
//
// 三，使用过程
// 1, 使用第一步编译出来的程序进行部署
// 2, 部署者导入e2_wallet文件，需要人工输入解密密码
// 3, 程序解密e2_wallet得到o_wallet到加载链程序内存中
// 4, 使用内存中的o_wallet进行原消息签名进行发送
// 5, TODO: 消息安全审计
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"strings"

	rice "github.com/GeertJohan/go.rice"
	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

var (
	// TODO: 发版时需要编译时写入, 以便可以各自指定
	rootPrivKey, _ = hex.DecodeString(strings.TrimSpace(rice.MustFindBox("../../../build/bootstrap").MustString("root.key")))
	rootPriv, _    = x509.ParsePKCS1PrivateKey(rootPrivKey)

	oldRootPriv *rsa.PrivateKey // nil for not exists.
)

func init() {
	oldKey, err := rice.MustFindBox("../../../build/bootstrap").String("root-old.key")
	if err != nil {
		// ignore
		return
	}

	oldRootPrivKey, err := hex.DecodeString(strings.TrimSpace(oldKey))
	if err != nil {
		log.Warn(errors.As(err, "root-old.key"))
		return
	}
	priv, err := x509.ParsePKCS1PrivateKey(oldRootPrivKey)
	if err != nil {
		log.Warn(err, "root-old.key")
		return
	}
	oldRootPriv = priv
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

// https://github.com/liamylian/x-rsa/blob/ebb3411a20ded20b3a1d47ee1b27d8fff3aeb24a/golang/xrsa/xrsa.go#L188
func split(buf []byte, lim int) [][]byte {
	var chunk []byte
	chunks := make([][]byte, 0, len(buf)/lim+1)
	for len(buf) >= lim {
		chunk, buf = buf[:lim], buf[lim:]
		chunks = append(chunks, chunk)
	}
	if len(buf) > 0 {
		chunks = append(chunks, buf[:])
	}
	return chunks
}

// echo for verifying version of the root's private key
func RootKeyHash() string {
	return fmt.Sprintf("%x", md5.Sum(rootPrivKey))
}

func GenRsaKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 4096)
}

// for export the root key
func EncodeRootKey(key *rsa.PrivateKey, passwd string) ([]byte, error) {
	blockType := "LOTUS ROOT KEY" // "RSA PRIVATE KEY"
	data := x509.MarshalPKCS1PrivateKey(key)
	alg := x509.PEMCipherAES256
	block, err := x509.EncryptPEMBlock(rand.Reader, blockType, data, []byte(passwd), alg)
	if err != nil {
		return nil, errors.As(err)
	}
	return pem.EncodeToMemory(block), nil
}

// for loading the root key
func DecodeRootKey(data []byte, passwd string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	data, err := x509.DecryptPEMBlock(block, []byte(passwd))
	if err != nil {
		return nil, errors.As(err)
	}
	return x509.ParsePKCS1PrivateKey(data)
}

func RSAEncript(src []byte, pub *rsa.PublicKey) (dst []byte, err error) {
	hash := sha512.New()
	pLen := pub.Size() - 2*hash.Size() - 2
	chunks := split(src, pLen)
	buffer := []byte{}
	for _, chunk := range chunks {
		eData, err := rsa.EncryptOAEP(hash, rand.Reader, pub, chunk, nil)
		if err != nil {
			return nil, err
		}
		buffer = append(buffer, eData...)
	}

	return buffer, nil
}

func RSADecript(src []byte, priv *rsa.PrivateKey) (dst []byte, err error) {
	hash := sha512.New()
	pLen := priv.PublicKey.Size()
	chunks := split(src, pLen)
	buffer := []byte{}
	for _, chunk := range chunks {
		dData, err := rsa.DecryptOAEP(hash, rand.Reader, priv, chunk, nil)
		if err != nil {
			return nil, err
		}
		buffer = append(buffer, dData...)
	}
	return buffer, nil
}

// https://docs.studygolang.com/src/crypto/x509/pem_decrypt.go?s=5819:5931#L186
func AESEncript(data []byte, passwd string) (dst []byte, err error) {
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
func AESDecript(src []byte, passwd string) (dst []byte, err error) {
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

func MixEncript(data []byte, passwd string) ([]byte, error) {
	e1Wallet, err := AESEncript(data, passwd)
	if err != nil {
		return nil, errors.As(err)
	}
	e2Wallet, err := RSAEncript(e1Wallet, &rootPriv.PublicKey)
	if err != nil {
		return nil, errors.As(err)
	}
	return e2Wallet, nil
}

func MixDecript(data []byte, passwd string) ([]byte, error) {
	e1Wallet, err := RSADecript(data, rootPriv)
	if err != nil {
		return nil, errors.As(err)
	}

	oWallet, err := AESDecript(e1Wallet, passwd)
	if err != nil {
		return nil, errors.As(err)
	}

	// return o_wallet
	return oWallet, nil
}

func MixOldDecript(data []byte, passwd string) ([]byte, error) {
	if oldRootPriv == nil {
		return nil, errors.New("no old root private key")
	}

	e1Wallet, err := RSADecript(data, oldRootPriv)
	if err != nil {
		return nil, errors.As(err)
	}

	oWallet, err := AESDecript(e1Wallet, passwd)
	if err != nil {
		return nil, errors.As(err)
	}

	// return o_wallet
	return oWallet, nil
}
