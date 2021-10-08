package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/gwaylib/errors"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/api.v7/kodo"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/syncdata/operation"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"
)

/*
if filesize<=4MB:
    hash = base64_url_safe(blkcnt, sha(file))
else:
    hash = base64_url_safe(blkcnt, sha(sha(blk0), sha(blk1)...))
字段含义
    blkcnt:文件以4MB为一个块进行切分后的块个数。
    blkN:第N(N>=0)个数据块的数据。
*/
var BSIZE = int64(32 * 1024 * 1024)

func ETag(file string, block_size int64) (etag string, err error) {

	f, err := os.Open(file)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	var blkcnt uint32 = uint32(fi.Size() / block_size)
	if fi.Size()%block_size != 0 {
		blkcnt += 1
	}

	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, blkcnt)

	h := sha1.New()
	buf := make([]byte, 0, 24)
	buf = append(buf, bs...)
	if fi.Size() <= block_size {
		io.Copy(h, f)
	} else {
		var i uint32
		for i = 0; i < blkcnt; i++ {
			shaBlk := sha1.New()
			io.Copy(shaBlk, io.LimitReader(f, block_size))
			io.Copy(h, bytes.NewReader(shaBlk.Sum(nil)))
		}
	}
	buf = h.Sum(buf)
	etag = base64.URLEncoding.EncodeToString(buf)
	return etag, nil
}

func ETagByBuffer(buffer *bytes.Buffer, block_size int64) (etag string, err error) {

	var size int64 = int64(buffer.Len())
	var blkcnt uint32 = uint32(size / block_size)
	if size%block_size != 0 {
		blkcnt += 1
	}

	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, blkcnt)

	h := sha1.New()
	buf := make([]byte, 0, 24)
	buf = append(buf, bs...)
	if size <= block_size {
		io.Copy(h, buffer)
	} else {
		var i uint32
		for i = 0; i < blkcnt; i++ {
			shaBlk := sha1.New()
			io.Copy(shaBlk, io.LimitReader(buffer, block_size))
			io.Copy(h, bytes.NewReader(shaBlk.Sum(nil)))
		}
	}
	buf = h.Sum(buf)
	etag = base64.URLEncoding.EncodeToString(buf)
	return etag, nil
}

//etags 是每个块的hash(hex 编码后的明文)，用逗号隔开
func ETagByBytes(data []byte, block_size int64) (etags string, etag string, err error) {

	etags = ""
	buffer := bytes.NewBuffer(data)
	var size int64 = int64(buffer.Len())
	var blkcnt uint32 = uint32(size / block_size)
	if size%block_size != 0 {
		blkcnt += 1
	}

	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, blkcnt)

	h := sha1.New()
	buf := make([]byte, 0, 24)
	buf = append(buf, bs...)
	if size <= block_size {
		io.Copy(h, buffer)
		etags = fmt.Sprintf("%s,", ShaBytes_Base64(data))
	} else {
		var i uint32
		for i = 0; i < blkcnt; i++ {
			shaBlk := sha1.New()
			io.Copy(shaBlk, io.LimitReader(buffer, block_size))
			io.Copy(h, bytes.NewReader(shaBlk.Sum(nil)))
			//etags = fmt.Sprintf("%s%x,", etags, shaBlk.Sum(nil))

			tmp := base64.URLEncoding.EncodeToString(shaBlk.Sum(nil))
			etags = fmt.Sprintf("%s%s,", etags, tmp)
			//etags = fmt.Sprintf("%s%x,", etags, shaBlk.Sum(nil))
		}
	}
	buf = h.Sum(buf)
	etag = base64.URLEncoding.EncodeToString(buf)

	if etags[len(etags)-1] == ',' {
		etags = etags[0 : len(etags)-1]
	}
	return etags, etag, nil
}

//etags 是每个块的hash(16进制 编码后的明文)， 默认4M一个分片
func ETagByEtags(etags []string) (etag string) {

	blkcnt := uint32(len(etags))
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, blkcnt)

	h := sha1.New()
	buf := make([]byte, 0, 24)
	buf = append(buf, bs...)
	if blkcnt == 1 && etags[0] != "" {
		//tmp_bytes, _ := hex.DecodeString(etags[0])
		tmp_bytes, _ := base64.URLEncoding.DecodeString(etags[0])
		buf = append(buf, tmp_bytes...)
	} else {
		var i uint32
		for i = 0; i < blkcnt; i++ {
			//tmp_bytes, _ := hex.DecodeString(etags[i])
			tmp_bytes, _ := base64.URLEncoding.DecodeString(etags[i])
			io.Copy(h, bytes.NewReader(tmp_bytes))
		}
		buf = h.Sum(buf)
	}

	etag = base64.URLEncoding.EncodeToString(buf)
	return etag
}

func ShaBytes(bytess []byte) (sha string) {
	sha = fmt.Sprintf("%x", sha1.Sum(bytess))
	return sha
}

func ShaBytes_Base64(bytess []byte) (sha string) {
	buf := sha1.Sum(bytess)
	sha = base64.URLEncoding.EncodeToString([]byte(buf[:]))
	return sha
}

func Md5Bytes(bytess []byte) string {
	h := md5.New()
	h.Write(bytess)
	out := h.Sum(nil)
	return fmt.Sprintf("%s", hex.EncodeToString(out))
}

func CheckMd5(bytess []byte, reqMd5 string, session string) bool {
	h := md5.New()
	h.Write(bytess)
	out := h.Sum(nil)
	if (reqMd5 == fmt.Sprintf("%s", hex.EncodeToString(out))) ||
		(reqMd5 == base64.StdEncoding.EncodeToString(out)) {
		return true
	}
	log.Infof("session: %+v,MD5 Wrong req_md5:%s, hex_md5:%s, base64_md5:%s", session, reqMd5, hex.EncodeToString(out), base64.StdEncoding.EncodeToString(out))
	return false
}

func EncodeEtagId(etag, session string) string {
	return etag + "@session_" + session
}

func DecodeEtagFromEtagId(etagid string) string {
	pos := strings.Index(etagid, "@session_")
	if pos != -1 {
		return etagid[0:pos]
	}
	return etagid
}

func ETagOneBlock(data []byte) (etag string, err error) {

	buffer := bytes.NewBuffer(data)

	h := sha1.New()
	buf := make([]byte, 0, 20)

	io.Copy(h, buffer)
	buf = h.Sum(buf)
	etag = base64.URLEncoding.EncodeToString(buf)

	return etag, nil
}

func Md5OneBlock(data []byte) (etag string, err error) {

	buffer := bytes.NewBuffer(data)

	h := md5.New()
	buf := make([]byte, 0, 20)

	io.Copy(h, buffer)
	buf = h.Sum(buf)
	etag = base64.URLEncoding.EncodeToString(buf)

	return etag, nil
}

func QiniuETagByBytes(data []byte, block_size int64) (etags string, etag string, err error) {

	etags = ""
	buffer := bytes.NewBuffer(data)
	var size int64 = int64(buffer.Len())
	var blkcnt uint32 = uint32(size / block_size)
	if size%block_size != 0 {
		blkcnt += 1
	}

	fmt.Println("cnt:", blkcnt)

	bs := new(bytes.Buffer)
	if blkcnt <= 1 {
		binary.Write(bs, binary.LittleEndian, uint8(0x16))
	} else {
		binary.Write(bs, binary.LittleEndian, uint8(0x96))
	}

	h := sha1.New()
	buf := make([]byte, 0, 21)
	buf = append(buf, bs.Bytes()...)
	if size <= block_size {
		io.Copy(h, buffer)
		etags = fmt.Sprintf("%s,", ShaBytes_Base64(data))
	} else {
		var i uint32
		for i = 0; i < blkcnt; i++ {
			shaBlk := sha1.New()
			io.Copy(shaBlk, io.LimitReader(buffer, block_size))
			io.Copy(h, bytes.NewReader(shaBlk.Sum(nil)))

			tmp := base64.URLEncoding.EncodeToString(shaBlk.Sum(nil))
			etags = fmt.Sprintf("%s%s,", etags, tmp)
		}
	}
	buf = h.Sum(buf)
	etag = base64.URLEncoding.EncodeToString(buf)

	if etags[len(etags)-1] == ',' {
		etags = etags[0 : len(etags)-1]
	}
	return etags, etag, nil
}

func QiniuETagByEtags_old(etags []string) (etag string) {

	blkcnt := uint32(len(etags))
	fmt.Println("cnt:", blkcnt)
	bs := new(bytes.Buffer)
	if blkcnt <= 1 {
		binary.Write(bs, binary.LittleEndian, uint8(0x16))
	} else {
		binary.Write(bs, binary.LittleEndian, uint8(0x96))
	}

	h := sha1.New()
	buf := make([]byte, 0, 21)
	buf = append(buf, bs.Bytes()...)
	if blkcnt == 1 && etags[0] != "" {
		tmp_bytes, _ := base64.URLEncoding.DecodeString(etags[0])
		buf = append(buf, tmp_bytes...)
	} else {
		var i uint32
		for i = 0; i < blkcnt; i++ {
			fmt.Println("etag i", i, etags[i])
			tmp_bytes, _ := base64.URLEncoding.DecodeString(etags[i])
			io.Copy(h, bytes.NewReader(tmp_bytes))
		}
		buf = h.Sum(buf)
	}

	etag = base64.URLEncoding.EncodeToString(buf)
	return etag
}

//往s3方向进化
func QiniuETagByEtags(etags []string) (etag string) {

	blkcnt := uint32(len(etags))

	h := md5.New()
	buf := make([]byte, 0, 21)
	if blkcnt == 1 && etags[0] != "" {
		tmp_bytes, _ := hex.DecodeString(etags[0])
		buf = append(buf, tmp_bytes...)
	} else {
		var i uint32
		for i = 0; i < blkcnt; i++ {
			tmp_bytes, _ := hex.DecodeString(etags[i])
			io.Copy(h, bytes.NewReader(tmp_bytes))
		}
		buf = h.Sum(buf)
	}

	etag = hex.EncodeToString(buf)
	return etag
}
func qiniu_etag(fname string) {
	file, err := os.Open(fname)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()

	fileinfo, err := file.Stat()
	buffer := make([]byte, BSIZE)
	etags := make([]string, int((fileinfo.Size()+BSIZE-1)/BSIZE))

	count := 0
	for {
		read_len, err := file.Read(buffer)
		if err != nil {
			if err != io.EOF {
				fmt.Println(err)
			}
			break
		}
		etags[count], _ = ETagOneBlock(buffer[0:read_len])
		count++
	}

	etag := QiniuETagByEtags_old(etags)
	fmt.Println("etag: ", etag)
}

func qiniu_etag_md5(fname string) {
	file, err := os.Open(fname)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()

	fileinfo, err := file.Stat()
	buffer := make([]byte, BSIZE)
	etags := make([]string, int((fileinfo.Size()+BSIZE-1)/BSIZE))

	count := 0
	for {
		read_len, err := file.Read(buffer)
		if err != nil {
			if err != io.EOF {
				fmt.Println(err)
			}
			break
		}
		etags[count], _ = ETagOneBlock(buffer[0:read_len])
		count++
	}

	etag := QiniuETagByEtags(etags)
	fmt.Println("etag: ", etag)
}

func qiniu_md5(fname string) {
	file, err := os.Open(fname)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()

	fileinfo, err := file.Stat()
	buffer := make([]byte, BSIZE)
	etags := make([]string, int((fileinfo.Size()+BSIZE-1)/BSIZE))

	count := 0
	for {
		read_len, err := file.Read(buffer)
		if err != nil {
			if err != io.EOF {
				fmt.Println(err)
			}
			break
		}
		etags[count], _ = Md5OneBlock(buffer[0:read_len])
		count++
	}

	etag := QiniuETagByEtags(etags)
	fmt.Println("etag: ", etag)
}

func qiniu_whold(fname string) {
	buf, err := ioutil.ReadFile(fname)
	if err != nil {
		fmt.Println(err)
	}

	_, e, err := QiniuETagByBytes(buf, BSIZE)
	fmt.Println("etag:", e)
}

func s3_etag(path string, partSize int) string {
	content, _ := ioutil.ReadFile(path)
	size := len(content)
	contentToHash := content
	parts := 0

	if size > partSize {
		pos := 0
		contentToHash = make([]byte, 0)
		for size > pos {
			endpos := pos + partSize
			if endpos >= size {
				endpos = size
			}
			hash := md5.Sum(content[pos:endpos])
			contentToHash = append(contentToHash, hash[:]...)
			pos += partSize
			parts += 1
		}
	}

	hash := md5.Sum(contentToHash)
	etag := fmt.Sprintf("%x", hash)
	if parts > 0 {
		etag += fmt.Sprintf("-%d", parts)
	}
	//fmt.Println(parts)
	fmt.Println(etag)
	return etag
}

func ComputeEtagLocal(filename string) (string, error) {
	return s3_etag(filename, int(BSIZE)), nil
}
func GetEtagFromServer(ctx context.Context, key string) (string, error) {
	up := os.Getenv("US3")
	if up == "" {
		return "", errors.New("US3 Config Not Found")
	}
	config, err := operation.Load(up)
	if err != nil {
		return "", err
	}
	cfg := kodo.Config{
		AccessKey: config.Ak,
		SecretKey: config.Sk,
		RSHost:    config.RsHosts[0],  //stat 主要用这个
		RSFHost:   config.RsfHosts[0], //列表主要靠这个配置
		UpHosts:   config.UpHosts,
	}
	client := kodo.NewWithoutZone(&cfg)
	bucket, err := client.BucketWithSafe(config.Bucket)
	if err != nil {
		return "", err
	}
	entry, err := bucket.Stat(ctx, key)
	if err != nil {
		return "", err
	}
	return entry.Hash, nil
}

//临时用list接口替代,list接口不是很稳定
func GetEtagFromServer2(ctx context.Context, key string) (string, error) {
	prefix := path.Dir(key)
	log.Infof("prefix: %+v ,key: %+v ", prefix, key)
	up := os.Getenv("US3")
	if up == "" {
		return "", errors.New("US3 Config Not Found")
	}
	config, err := operation.Load(up)
	if err != nil {
		return "", err
	}
	cfg := kodo.Config{
		AccessKey: config.Ak,
		SecretKey: config.Sk,
		RSHost:    config.RsHosts[0],  //stat 主要用这个
		RSFHost:   config.RsfHosts[0], //列表主要靠这个配置
		UpHosts:   config.UpHosts,
	}
	client := kodo.NewWithoutZone(&cfg)
	bucket, err := client.BucketWithSafe(config.Bucket)
	if err != nil {
		return "", err
	}
	marker := ""
	hash := ""
	for {
		r, _, out, err := bucket.List(nil, prefix, "", marker, 1000)
		if err != nil && err != io.EOF {
			time.Sleep(time.Second)
			log.Errorf("get file %+v etag err: %+v", key, err.Error())
			continue
		}
		for _, v := range r {
			if strings.EqualFold(v.Key, key) {
				hash = v.Hash
			}
		}
		if out == "" {
			break
		}
		marker = out
	}
	if hash != "" {
		return hash, nil
	}
	return "", errors.New(fmt.Sprintf("file: %+v get etag error", key))
}
