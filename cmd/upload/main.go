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
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper/basicfs"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
	logging "github.com/ipfs/go-log/v2"
	"github.com/mitchellh/go-homedir"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/api.v7/kodo"
	"github.com/ufilesdk-dev/us3-qiniu-go-sdk/syncdata/operation"
	"github.com/urfave/cli/v2"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	QINIU_VIRTUAL_MOUNTPOINT = "/data/oss/qiniu/"
)

var log = logging.Logger("lotus-bench")

func main() {
	logging.SetLogLevel("*", "INFO")

	log.Info("Starting lotus-bench")

	app := &cli.App{
		Name:    "upload-oss",
		Usage:   "upload files to oss",
		Version: build.UserVersion(),
		Commands: []*cli.Command{
			uploadCmd,
		},
	}
	if err := app.Run(os.Args); err != nil {
		log.Warnf("%+v", err)
		return
	}
}

var uploadCmd = &cli.Command{
	Name:      "upload",
	Usage:     "Benchmark a proof computation",
	ArgsUsage: "",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "miner-addr",
			Usage: "pass miner address (only necessary if using existing sectorbuilder)",
			Value: "f01246563",
		},
		&cli.Uint64Flag{
			Name:  "sector-number",
			Usage: "select number of sectors to seal",
			Value: 398,
		},
		&cli.StringFlag{
			Name:  "storage-dir",
			Usage: "select number of sectors to seal",
			Value: "/data/cache/1",
		},
		&cli.StringFlag{
			Name:  "config",
			Usage: "select number of sectors to seal",
			Value: "/root/hlm-miner/etc/cfg.toml",
		},
	},
	Action: func(c *cli.Context) error {
		// miner address
		os.Setenv("US3", c.String("config"))
		sdir, err := homedir.Expand(c.String("storage-dir"))
		if err != nil {
			log.Error(err)
			return err
		}
		maddr, err := address.NewFromString(c.String("miner-addr"))
		if err != nil {
			log.Error(err)
			return err
		}
		amid, err := address.IDFromAddress(maddr)
		if err != nil {
			log.Error(err)
			return err
		}
		mid := abi.ActorID(amid)
		sid := storage.SectorName(abi.SectorID{
			Number: abi.SectorNumber(c.Uint64("sector-number")),
			Miner:  mid,
		})
		mountDir := filepath.Join(QINIU_VIRTUAL_MOUNTPOINT, sid)
		// send the sealed
		sealer, err := ffiwrapper.New(ffiwrapper.RemoteCfg{}, &basicfs.Provider{
			Root: sdir,
		})
		if err != nil {
			log.Error(err)
			return err
		}
		sealedFromPath := sealer.SectorPath("sealed", sid)
		sealedToPath := filepath.Join(mountDir, "sealed")
		w := worker{}
		if err := w.upload(context.TODO(), sealedFromPath, filepath.Join(sealedToPath, sid)); err != nil {
			log.Error(err)
		}
		// send the cache
		cacheFromPath := sealer.SectorPath("cache", sid)
		cacheToPath := filepath.Join(mountDir, "cache", sid)
		if err := w.upload(context.TODO(), cacheFromPath, cacheToPath); err != nil {
			log.Error(err)
		}
		return nil
	},
}

type worker struct {
}

func (w *worker) upload(ctx context.Context, fromPath, toPath string) error {
	stat, err := os.Stat(fromPath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(fromPath)
		}
		return err
	}

	log.Infof("upload from: %s, to: %s", fromPath, toPath)
	if stat.IsDir() {
		if err := CopyFile(ctx, fromPath+"/", toPath+"/", NewTransferer(travelFile, uploadToOSS)); err != nil {
			return err
		}
	} else {
		if err := CopyFile(ctx, fromPath, toPath, NewTransferer(travelFile, uploadToOSS)); err != nil {
			return err
		}
	}
	return nil
}

type Transferer struct {
	travel func(path string) (os.FileInfo, []string, error)
	copy   func(ctx context.Context, from, to string) error
}

func NewTransferer(t func(path string) (os.FileInfo, []string, error), h func(ctx context.Context, from, to string) error) *Transferer {
	tran := &Transferer{
		travel: t,
		copy:   h,
	}
	return tran
}

func CopyFile(ctx context.Context, from, to string, t ...*Transferer) error {
	copyfunc := &Transferer{copy: copyFile, travel: travelFile}
	if len(t) != 0 {
		copyfunc = t[0]
	}
	_, source, err := copyfunc.travel(from)
	if err != nil {
		return errors.As(err)
	}

	for _, src := range source {
		toFile := strings.Replace(src, from, to, 1)
		tCtx, cancel := context.WithTimeout(ctx, time.Hour)
		if err := copyfunc.copy(tCtx, src, toFile); err != nil {
			cancel()
			log.Warn(errors.As(err))

			// try again
			tCtx, cancel = context.WithTimeout(ctx, time.Hour)
			if err := copyfunc.copy(tCtx, src, toFile); err != nil {
				cancel()
				return errors.As(err)
			}
		}
		cancel()
	}
	return nil
}

func travelFileEmpty(path string) (os.FileInfo, []string, error) {
	return nil, []string{path}, nil
}

func travelFile(path string) (os.FileInfo, []string, error) {
	fStat, err := os.Lstat(path)
	if err != nil {
		return nil, nil, errors.As(err, path)
	}
	if !fStat.IsDir() {
		return nil, []string{path}, nil
	}
	dirs, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, nil, errors.As(err)
	}
	result := []string{}
	for _, fs := range dirs {
		filePath := filepath.Join(path, fs.Name())
		if !fs.IsDir() {
			result = append(result, filePath)
			continue
		}
		_, nextFiles, err := travelFile(filePath)
		if err != nil {
			return nil, nil, errors.As(err, filePath)
		}
		result = append(result, nextFiles...)
	}
	return fStat, result, nil
}

const (
	append_file_new       = 0
	append_file_continue  = 1
	append_file_completed = 2
)

func checksumFile(aFile, bFile *os.File) (int, error) {
	if _, err := aFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}

	// checksum all data
	ah := sha1.New()
	if _, err := io.Copy(ah, aFile); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	aSum := ah.Sum(nil)

	bh := sha1.New()
	if _, err := io.Copy(bh, bFile); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}
	bSum := bh.Sum(nil)

	if !bytes.Equal(aSum, bSum) {
		return append_file_new, nil
	}

	return append_file_completed, nil
}
func canAppendFile(aFile, bFile *os.File, aStat, bStat os.FileInfo) (int, error) {
	checksumSize := int64(32 * 1024)
	// for small size, just do rewrite.
	aSize := aStat.Size()
	bSize := bStat.Size()
	if bSize < checksumSize {
		return append_file_new, nil
	}
	if bSize > aSize {
		return append_file_new, nil
	}

	// checksum the end
	aData := make([]byte, checksumSize)
	bData := make([]byte, checksumSize)
	if _, err := aFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.Seek(0, 0); err != nil {
		return append_file_new, errors.As(err, bFile.Name())
	}
	if _, err := aFile.ReadAt(aData, bSize-checksumSize); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if _, err := bFile.ReadAt(bData, bSize-checksumSize); err != nil {
		return append_file_new, errors.As(err, aFile.Name())
	}
	if !bytes.Equal(aData, bData) {
		return append_file_new, nil
	}
	if aSize > bSize {
		return append_file_continue, nil
	}

	return append_file_completed, nil
}
func copyFile(ctx context.Context, from, to string) error {
	if from == to {
		return errors.New("Same file").As(from, to)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return errors.As(err, to)
	}
	fromFile, err := os.Open(from)
	if err != nil {
		return errors.As(err, from)
	}
	defer fromFile.Close()
	fromStat, err := fromFile.Stat()
	if err != nil {
		if os.IsNotExist(err) {
			return errors.ErrNoData.As(to)
		}
		return errors.As(err, to)
	}

	// TODO: make chtime
	toFile, err := os.OpenFile(to, os.O_RDWR|os.O_CREATE, fromStat.Mode())
	if err != nil {
		return errors.As(err, to, uint64(fromStat.Mode()))
	}
	defer toFile.Close()
	toStat, err := toFile.Stat()
	if err != nil {
		return errors.As(err, to)
	}

	// checking continue
	stats, err := canAppendFile(fromFile, toFile, fromStat, toStat)
	if err != nil {
		return errors.As(err)
	}
	switch stats {
	case append_file_completed:
		// has done
		fmt.Printf("%s ======= completed\n", to)
		return nil

	case append_file_continue:
		appendPos := int64(toStat.Size() - 1)
		if appendPos < 0 {
			appendPos = 0
			fmt.Printf("%s ====== new \n", to)
		} else {
			fmt.Printf("%s ====== continue: %d\n", to, appendPos)
		}
		if _, err := fromFile.Seek(appendPos, 0); err != nil {
			return errors.As(err)
		}
		if _, err := toFile.Seek(appendPos, 0); err != nil {
			return errors.As(err)
		}
	default:
		fmt.Printf("%s ====== new \n", to)
		// TODO: allow truncate, current need to delete the files by manully.
		if err := toFile.Truncate(0); err != nil {
			return errors.As(err)
		}
		if _, err := toFile.Seek(0, 0); err != nil {
			return errors.As(err)
		}
		if _, err := fromFile.Seek(0, 0); err != nil {
			return errors.As(err)
		}
	}

	errBuff := make(chan error, 1)
	interrupt := false
	iLock := sync.Mutex{}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			iLock.Lock()
			if interrupt {
				iLock.Unlock()
				return
			}
			iLock.Unlock()

			nr, er := fromFile.Read(buf)
			if nr > 0 {
				nw, ew := toFile.Write(buf[0:nr])
				if ew != nil {
					errBuff <- errors.As(ew)
					return
				}
				if nr != nw {
					errBuff <- errors.As(io.ErrShortWrite)
					return
				}
			}
			if er != nil {
				errBuff <- errors.As(er)
				return
			}
		}
	}()
	select {
	case err := <-errBuff:
		if !errors.Equal(err, io.EOF) {
			return errors.As(err)
		}
		stats, err := checksumFile(fromFile, toFile)
		if err != nil {
			return errors.As(err)
		}
		if stats == append_file_completed {
			return nil
		}
		// TODO: allow truncate, current need to delete the files by manully.
		//if err := toFile.Truncate(0); err != nil {
		//	return errors.As(err, toFile)
		//}
		//if _, err := toFile.Seek(0, 0); err != nil {
		//	return errors.As(err, toFile)
		//}
		return errors.New("finalize has completed, but checksum failed.").As(stats, from, to, fromStat.Size(), toStat.Size())
	case <-ctx.Done():
		iLock.Lock()
		interrupt = true
		iLock.Unlock()
		return ctx.Err()
	}
}

// upload file from filesystem to us3 oss cluster
func uploadToOSS(ctx context.Context, from, to string) error {
	up := os.Getenv("US3")
	if up == "" {
		log.Info("please set US3 environment variable first!")
		return errors.New("connot find US3 environment variable")
	}
	conf2, err := operation.Load(up)
	if err != nil {
		log.Error("load config error", err)
		return errors.As(err)
	}
	uploader := operation.NewUploaderV2()
	log.Infof("start upload :  %s to %s ", from, to)
	timeStart := time.Now()
	err = uploader.Upload(from, to)
	if err != nil {
		return errors.As(err)
	}
	timeEnd := time.Now()
	log.Infof("file  upload  %s to %s ,start :%+v, end: %+v, last:%+v ", from, to, timeStart, timeEnd, timeEnd.Sub(timeStart))
	etagLocal, err := ComputeEtagLocal(from)
	if err != nil {
		return errors.As(err)
	}
	log.Infof("etagLocal: %+v", etagLocal)
	etagRemote, err := GetEtagFromServer2(ctx, to[1:])
	if err != nil {
		return errors.As(err)
	}
	if !strings.EqualFold(etagLocal, etagRemote) {
		return errors.New(fmt.Sprintf("file %+v etag not match: local: %+v ,remote: %+v", from, etagLocal, etagRemote))
	}
	log.Infof("finish upload :  %s to %s ,err: %+v  ", from, to, err)
	if conf2.Delete {
		if err == nil {
			os.Remove(from)
		}
	}
	return err
}

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
