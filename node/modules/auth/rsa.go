// 生成过程
// 一，编译
// 1, 独立制作程序根证书, 根证书一般由编译人员掌握
// 2, 编译出程序
// 3, 当根证书变更时，必须对所有钱包密钥变更才能运行, 因此正常情况下根证书不需变更。
//
// 二，加密钱包
// 1, 使用前面编译的程序，独立生成加密指令
// 2, 内存创建原始钱包密钥(三个地址，o_wallet)
// 3, 加密机生成加密所需的非对称密钥对, e1_priv_key, e1_pub_key
// 4, 使用e1_pub_key证书对o_wallet进行加密得到加密的钱包(e_wallet)
// 5, 通过根证书加密e1_priv_key，得到e2_priv_key
// 6, 通过AES256加密e2_priv_key，导出得到e3_priv_key部署文件
// 7, 导出e1_wallet得到e1_wallet部署文件(三个文件，对应三个钱包地址)
//
// 三，使用过程
// 1, 部署四个加密文件到链服务器的硬盘上, 并使用第一步编译的程序进行部署
// 2, 部署者加载e3_priv_key到内存中，要求每次加密需要人工输入解密密码
// 3, 程序得到e2_priv_key开始对e_wallet解密得到o_wallet到链程序内存中
// 4, 使用内存中的o_wallet进行原消息签名进行发送
// 5, TODO: 消息安全审计
package auth

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/gwaylib/errors"
)

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

func EncodeRSAKey(key *rsa.PrivateKey, passwd string) ([]byte, error) {
	blockType := "WALLET PRIVATE KEY" // "RSA PRIVATE KEY"
	data := x509.MarshalPKCS1PrivateKey(key)

	e2Data, err := RSAEncript(data, &rootPriv.PublicKey)
	if err != nil {
		return nil, errors.As(err)
	}

	alg := x509.PEMCipherAES256
	block, err := x509.EncryptPEMBlock(rand.Reader, blockType, e2Data, []byte(passwd), alg)
	if err != nil {
		return nil, errors.As(err)
	}

	// return the e3_priv_key data
	return pem.EncodeToMemory(block), nil
}
func DecodeRSAKey(data []byte, passwd string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	e2Data, err := x509.DecryptPEMBlock(block, []byte(passwd))
	if err != nil {
		return nil, errors.As(err)
	}
	e1Data, err := RSADecript(e2Data, rootPriv)
	if err != nil {
		return nil, errors.As(err)
	}

	// return the e1_prive_key
	return x509.ParsePKCS1PrivateKey(e1Data)
}
