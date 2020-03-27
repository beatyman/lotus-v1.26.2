package main

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"os"

	"path/filepath"

	sectorbuilder "github.com/filecoin-project/go-sectorbuilder"
	"github.com/filecoin-project/go-sectorbuilder/fs"
	"github.com/gwaylib/errors"
	files "github.com/ipfs/go-ipfs-files"
	"golang.org/x/xerrors"
	"gopkg.in/cheggaaa/pb.v1"

	"github.com/filecoin-project/lotus/lib/tarutil"
)

func (w *worker) sizeForType(typ string) int64 {
	size := int64(w.sb.SectorSize())
	if typ == "cache" {
		size *= 10
	}
	return size
}

func (w *worker) fetch(typ string, sectorID uint64) error {
	// Close the fetch in the miner storage directory.
	// TODO: fix to env
	if filepath.Base(w.repo) == ".lotusstorage" {
		return nil
	}

	outname := filepath.Join(w.repo, typ, w.sb.SectorName(sectorID))

	url := w.minerEndpoint + "/remote/" + typ + "/" + fmt.Sprint(sectorID)
	log.Infof("Fetch %s %s", typ, url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return xerrors.Errorf("request: %w", err)
	}
	req.Header = w.auth

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return xerrors.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return xerrors.Errorf("non-200 code: %d", resp.StatusCode)
	}

	bar := pb.New64(w.sizeForType(typ))
	bar.ShowPercent = true
	bar.ShowSpeed = true
	bar.Units = pb.U_BYTES

	barreader := bar.NewProxyReader(resp.Body)

	bar.Start()
	defer bar.Finish()

	mediatype, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return xerrors.Errorf("parse media type: %w", err)
	}

	if err := os.RemoveAll(outname); err != nil {
		return xerrors.Errorf("removing dest: %w", err)
	}

	switch mediatype {
	case "application/x-tar":
		return tarutil.ExtractTar(barreader, outname)
	case "application/octet-stream":
		return files.WriteTo(files.NewReaderFile(barreader), outname)
	default:
		return xerrors.Errorf("unknown content type: '%s'", mediatype)
	}

}

func (w *worker) push(ctx context.Context, typ string, sectorID uint64) error {
	// Close the fetch in the miner storage directory.
	// TODO: fix to env
	if filepath.Base(w.repo) == ".lotusstorage" {
		return nil
	}

	fromPath, err := w.sb.SectorPath(fs.DataType(typ), sectorID)
	if err != nil {
		return errors.As(err, fs.DataType(typ), sectorID)
	}

	stat, err := os.Stat(string(fromPath))
	if err != nil {
		return err
	}

	// save to target storage
	toPath, err := w.sealedSB.SectorPath(fs.DataType(typ), sectorID)
	if err != nil {
		return errors.As(err, fs.DataType(typ), sectorID)
	}

	if stat.IsDir() {
		if err := CopyFile(ctx, string(fromPath)+"/", string(toPath)+"/"); err != nil {
			return err
		}
	} else {
		if err := CopyFile(ctx, string(fromPath), string(toPath)); err != nil {
			return err
		}
	}

	// url := w.minerEndpoint + "/remote/" + typ + "/" + fmt.Sprint(sectorID)
	// log.Infof("Push URL: %s %s", typ, url)
	// header := w.auth
	// req, err := http.NewRequest("PUT", url, bar.NewProxyReader(r))
	// if err != nil {
	// 	return err
	// }
	// req.Header = header
	//
	// resp, err := http.DefaultClient.Do(req)
	// if err != nil {
	// 	return err
	// }
	// if resp.StatusCode != 200 {
	// 	return xerrors.Errorf("non-200 response: %d", resp.StatusCode)
	// }
	//
	// if err := resp.Body.Close(); err != nil {
	// 	return err
	// }

	// TODO: keep files around for later stages of sealing
	// return w.remove(typ, sectorID)
	return nil
}

func (w *worker) remove(typ string, sectorID uint64) error {
	filename := filepath.Join(w.repo, typ, w.sb.SectorName(sectorID))
	log.Infof("Remove file: %s", filename)
	return os.RemoveAll(filename)
}

func (w *worker) fetchSector(sectorID uint64, typ sectorbuilder.WorkerTaskType) error {
	var err error
	switch typ {
	case sectorbuilder.WorkerPreCommit:
		err = w.fetch("staging", sectorID)
	case sectorbuilder.WorkerCommit:
		err = w.fetch("sealed", sectorID)
		if err != nil {
			return xerrors.Errorf("fetch sealed: %w", err)
		}
		err = w.fetch("cache", sectorID)
	}
	if err != nil {
		return xerrors.Errorf("fetch failed: %w", err)
	}
	return nil
}
