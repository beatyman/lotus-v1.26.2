package unixfs

import (
	"archive/tar"
	"bufio"
	"context"
	"fmt"
	"github.com/filecoin-project/go-commp-utils/writer"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/google/uuid"
	"github.com/gwaylib/errors"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/filecoin-project/go-fil-markets/stores"
	"github.com/ipfs/boxo/files"
	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-cidutil"
	bstore "github.com/ipfs/go-ipfs-blockstore"
	chunker "github.com/ipfs/go-ipfs-chunker"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-unixfs/importer/balanced"
	ihelper "github.com/ipfs/go-unixfs/importer/helpers"
	"github.com/ipld/go-car"
	mh "github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/build"
)

var DefaultHashFunction = uint64(mh.BLAKE2B_MIN + 31)

func CidBuilder() (cid.Builder, error) {
	prefix, err := merkledag.PrefixForCidVersion(1)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize UnixFS CID Builder: %w", err)
	}
	prefix.MhType = DefaultHashFunction
	b := cidutil.InlineBuilder{
		Builder: prefix,
		Limit:   126,
	}
	return b, nil
}

// CreateFilestore takes a standard file whose path is src, forms a UnixFS DAG, and
// writes a CARv2 file with positional mapping (backed by the go-filestore library).
func CreateFilestore(ctx context.Context, srcPath, dstPath, cacheDir string) (cid.Cid, error) {
	// This method uses a two-phase approach with a staging CAR blockstore and
	// a final CAR blockstore.
	//
	// This is necessary because of https://github.com/ipld/go-car/issues/196
	//
	// TODO: do we need to chunk twice? Isn't the first output already in the
	//  right order? Can't we just copy the CAR file and replace the header?

	src, err := os.Open(srcPath)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to open input file: %w", err)
	}
	defer src.Close() //nolint:errcheck

	stat, err := src.Stat()
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to stat file :%w", err)
	}

	file, err := files.NewReaderPathFile(srcPath, src, stat)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create reader path file: %w", err)
	}

	f, err := ioutil.TempFile(cacheDir, "")
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create temp file: %w", err)
	}
	_ = f.Close() // close; we only want the path.

	tmp := f.Name()
	defer os.Remove(tmp) //nolint:errcheck

	// Step 1. Compute the UnixFS DAG and write it to a CARv2 file to get
	// the root CID of the DAG.
	fstore, err := stores.ReadWriteFilestore(tmp)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create temporary filestore: %w", err)
	}

	finalRoot1, err := Build(ctx, file, fstore, true)
	if err != nil {
		_ = fstore.Close()
		return cid.Undef, xerrors.Errorf("failed to import file to store to compute root: %w", err)
	}

	if err := fstore.Close(); err != nil {
		return cid.Undef, xerrors.Errorf("failed to finalize car filestore: %w", err)
	}

	// Step 2. We now have the root of the UnixFS DAG, and we can write the
	// final CAR for real under `dst`.
	bs, err := stores.ReadWriteFilestore(dstPath, finalRoot1)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create a carv2 read/write filestore: %w", err)
	}

	// rewind file to the beginning.
	if _, err := src.Seek(0, 0); err != nil {
		return cid.Undef, xerrors.Errorf("failed to rewind file: %w", err)
	}

	finalRoot2, err := Build(ctx, file, bs, true)
	if err != nil {
		_ = bs.Close()
		return cid.Undef, xerrors.Errorf("failed to create UnixFS DAG with carv2 blockstore: %w", err)
	}

	if err := bs.Close(); err != nil {
		return cid.Undef, xerrors.Errorf("failed to finalize car blockstore: %w", err)
	}

	if finalRoot1 != finalRoot2 {
		return cid.Undef, xerrors.New("roots do not match")
	}

	return finalRoot1, nil
}

// Build builds a UnixFS DAG out of the supplied reader,
// and imports the DAG into the supplied service.
func Build(ctx context.Context, reader io.Reader, into bstore.Blockstore, filestore bool) (cid.Cid, error) {
	b, err := CidBuilder()
	if err != nil {
		return cid.Undef, err
	}

	bsvc := blockservice.New(into, offline.Exchange(into))
	dags := merkledag.NewDAGService(bsvc)
	bufdag := ipld.NewBufferedDAG(ctx, dags)

	params := ihelper.DagBuilderParams{
		Maxlinks:   build.UnixfsLinksPerLevel,
		RawLeaves:  true,
		CidBuilder: b,
		Dagserv:    bufdag,
		NoCopy:     filestore,
	}

	db, err := params.New(chunker.NewSizeSplitter(reader, int64(build.UnixfsChunkSize)))
	if err != nil {
		return cid.Undef, err
	}
	nd, err := balanced.Layout(db)
	if err != nil {
		return cid.Undef, err
	}

	if err := bufdag.Commit(); err != nil {
		return cid.Undef, err
	}

	return nd.Cid(), nil
}

type CarFile struct {
	*car.CarHeader
	Path string
}

func GenCarFile(ctx context.Context, cacheDir, outputFile string, srcFiles []string) (*CarFile, error) {
	if len(srcFiles) == 0 {
		return nil, errors.New("no files input")
	}
	cacheAbs, err := filepath.Abs(cacheDir)
	if err != nil {
		return nil, errors.As(err)
	}
	if len(outputFile) == 0 {
		outputFile = filepath.Join(cacheAbs, uuid.New().String()+".car")
	}

	isTar := false
	srcFile := filepath.Join(cacheAbs, uuid.New().String()+".tar")
	defer func() {
		if isTar {
			os.Remove(srcFile)
		}
	}()
	if len(srcFiles) == 1 {
		srcFile = srcFiles[0]
		fi, err := os.Open(srcFile)
		if err != nil {
			return nil, errors.As(err)
		}
		// check that the data is a car file; if it's not, retrieval won't work
		if _, err := car.ReadHeader(bufio.NewReader(fi)); err == nil {
			fi.Close()
			return nil, errors.New("it's car file already")
		}
		fi.Close()
	} else {
		isTar = true
		cf, err := os.Create(srcFile)
		if err != nil {
			return nil, errors.As(err)
		}
		tw := tar.NewWriter(cf)

		// read files
		copyFileToBuf := func(file string) error {
			fi, err := os.Open(file)
			if err != nil {
				return errors.As(err)
			}
			defer fi.Close()

			stat, err := fi.Stat()
			if err != nil {
				return errors.As(err)
			}

			hdr := &tar.Header{
				Name:    stat.Name(),
				Mode:    int64(stat.Mode()),
				Size:    stat.Size(),
				ModTime: stat.ModTime(),
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return errors.As(err)
			}
			if _, err := io.Copy(tw, fi); err != nil {
				return errors.As(err)
			}
			return nil
		}
		for _, f := range srcFiles {
			if len(f) == 0 {
				continue
			}
			if err := copyFileToBuf(f); err != nil {
				cf.Close()
				return nil, errors.As(err)
			}
		}
		if err := tw.Flush(); err != nil {
			cf.Close()
			return nil, errors.As(err)
		}
		if err := tw.Close(); err != nil {
			cf.Close()
			return nil, errors.As(err)
		}
		cf.Close()
	}

	//////////////
	// make car
	//////////////
	tmpCarFile := filepath.Join(cacheDir, uuid.New().String()+".tmpcar")
	root, err := CreateFilestore(ctx, srcFile, tmpCarFile, cacheAbs)
	if err != nil {
		return nil, errors.As(err)
	}
	defer os.Remove(tmpCarFile)

	fs, err := stores.ReadOnlyFilestore(tmpCarFile)
	if err != nil {
		return nil, errors.As(err)
	}
	defer fs.Close() //nolint:errcheck

	f, err := os.Create(outputFile)
	if err != nil {
		return nil, errors.As(err)
	}
	defer f.Close()

	// build a dense deterministic CAR (dense = containing filled leaves)
	if err := car.NewSelectiveCar(
		ctx,
		fs,
		[]car.Dag{{
			Root:     root,
			Selector: selectorparse.CommonSelector_ExploreAllRecursively,
		}},
		car.MaxTraversalLinks(config.MaxTraversalLinks),
	).Write(
		f,
	); err != nil {
		return nil, errors.As(err, tmpCarFile, outputFile)
	}

	return &CarFile{
		CarHeader: &car.CarHeader{
			Version: 2,
			Roots:   []cid.Cid{root},
		},
		Path: outputFile,
	}, nil

}

func GenMemCar(ctx context.Context, srcPath, dstPath, cacheDir string) (*car.CarHeader, error) {
	//////////////
	// make car
	//////////////
	tmpCarFile := filepath.Join(cacheDir, uuid.New().String())
	root, err := CreateFilestore(ctx, srcPath, tmpCarFile, cacheDir)
	if err != nil {
		return nil, errors.As(err)
	}
	defer os.Remove(tmpCarFile)

	fs, err := stores.ReadOnlyFilestore(tmpCarFile)
	if err != nil {
		return nil, errors.As(err)
	}
	defer fs.Close() //nolint:errcheck

	f, err := os.Create(dstPath)
	if err != nil {
		return nil, errors.As(err)
	}
	defer f.Close()

	// build a dense deterministic CAR (dense = containing filled leaves)
	if err := car.NewSelectiveCar(
		ctx,
		fs,
		[]car.Dag{{
			Root:     root,
			Selector: selectorparse.CommonSelector_ExploreAllRecursively,
		}},
		car.MaxTraversalLinks(config.MaxTraversalLinks),
	).Write(
		f,
	); err != nil {
		return nil, errors.As(err)
	}
	return &car.CarHeader{
		Version: 2,
		Roots:   []cid.Cid{root},
	}, nil
}

func GenCommP(ctx context.Context, rdr io.ReadSeeker) (*writer.DataCIDSize, error) {
	//////////////
	// make commP
	//////////////
	// check that the data is a car file; if it's not, retrieval won't work
	if _, err := car.ReadHeader(bufio.NewReader(rdr)); err != nil {
		return nil, xerrors.Errorf("not a car file: %w", err)
	}
	// check that the data is a car file; if it's not, retrieval won't work
	if _, err := rdr.Seek(0, io.SeekStart); err != nil {
		return nil, xerrors.Errorf("seek to start: %w", err)
	}

	w := &writer.Writer{}
	if _, err := io.CopyBuffer(w, rdr, make([]byte, writer.CommPBuf)); err != nil {
		return nil, xerrors.Errorf("copy into commp writer: %w", err)
	}

	commp, err := w.Sum()
	if err != nil {
		return nil, xerrors.Errorf("computing commP failed: %w", err)
	}
	// release commp buffer
	w = nil
	return &commp, nil
}
