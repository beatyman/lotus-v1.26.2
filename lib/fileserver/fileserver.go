package fileserver

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/filecoin-project/lotus/extern/sector-storage/stores"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/gorilla/mux"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

var log = logging.Logger("fileserver")

var (
	_conns   int
	_connMux sync.Mutex
)

type StorageFileServer struct {
	repo   string
	token  string
	router *mux.Router
	next   http.Handler
}

type StorageDirectory struct {
	Href  string `xml:"href,attr"`
	Value string `xml:",chardata"`
}
type StorageDirectoryResp struct {
	XMLName xml.Name           `xml:"pre"`
	Files   []StorageDirectory `xml:"a"`
}
type fileHandle struct {
	handler http.Handler
}

func (f *fileHandle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: auth
	addConns(1)
	defer addConns(-1)
	f.handler.ServeHTTP(w, r)
}

func NewStorageFileServer(repo string) *StorageFileServer {
	mu := mux.NewRouter()
	paramsPath := os.Getenv("FIL_PROOFS_PARAMETER_CACHE")
	if len(paramsPath) == 0 {
		paramsPath = "/var/tmp/filecoin-proof-parameters"
	}
	//test
	mu.PathPrefix("/file/filecoin-proof-parameters").Handler(http.StripPrefix(
		"/file/filecoin-proof-parameters",
		&fileHandle{handler: http.FileServer(http.Dir(paramsPath))}, // TODO: get from env
	))
	mu.PathPrefix("/file/storage/cache").Handler(http.StripPrefix(
		"/file/storage/cache",
		&fileHandle{handler: http.FileServer(http.Dir(filepath.Join(repo, "cache")))},
	))
	mu.PathPrefix("/file/storage/unsealed").Handler(http.StripPrefix(
		"/file/storage/unsealed",
		&fileHandle{handler: http.FileServer(http.Dir(filepath.Join(repo, "unsealed")))},
	))
	mu.PathPrefix("/file/storage/sealed").Handler(http.StripPrefix(
		"/file/storage/sealed",
		&fileHandle{handler: http.FileServer(http.Dir(filepath.Join(repo, "sealed")))},
	))

	mu.PathPrefix("/file/storage/delete").Handler(http.StripPrefix(
		"/file/storage/delete",
		&fileHandle{
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("flags", "done")
				sid := r.FormValue("sid")
				sectorType := r.FormValue("type")
				_, err := parseSectorID(sid)
				if err != nil {
					w.WriteHeader(404)
					w.Write([]byte("sector id no support"))
				}

				ft, err := ftFromString(sectorType)
				if err != nil {
					w.WriteHeader(404)
					w.Write([]byte("sector type not support"))
					return
				}
				if ft == "all" {
					//log.Infof("try delete cache:%s", sid)
					if err := os.RemoveAll(filepath.Join(repo, "cache", sid)); err != nil {
						w.WriteHeader(500)
						w.Write([]byte("delete cache failed:" + err.Error()))
						return
					}
					if err := os.RemoveAll(filepath.Join(repo, "sealed", sid)); err != nil {
						w.WriteHeader(500)
						w.Write([]byte("delete sealed failed:" + err.Error()))
						return
					}
					if err := os.RemoveAll(filepath.Join(repo, "unsealed", sid)); err != nil {
						w.WriteHeader(500)
						w.Write([]byte("delete unsealed failed:" + err.Error()))
						return
					}
				} else {
					path := filepath.Join(repo, ft, sid)
					if err := os.RemoveAll(path); err != nil {
						w.WriteHeader(500)
						w.Write([]byte("delete cache failed:" + err.Error()))
						return
					}
				}
			}), // end HandlerFunc
		}, // end handler
	))

	return &StorageFileServer{
		repo:   repo,
		router: mu,
	}
}

func Conns() int {
	_connMux.Lock()
	defer _connMux.Unlock()
	return _conns
}
func addConns(n int) {
	_connMux.Lock()
	defer _connMux.Unlock()
	_conns += n
}

func (f *StorageFileServer) FileHttpServer(w http.ResponseWriter, r *http.Request) {
	// TODO: auth
	//data, _ := httputil.DumpRequest(r, true)
	//log.Info(string(data))
	//// auth
	//unVerify := auth.Permission("NoVerify")
	//if auth.HasPerm(r.Context(), []auth.Permission{unVerify}, unVerify) {
	//	w.WriteHeader(401)
	//	return
	//}

	f.router.ServeHTTP(w, r)
}

func parseSectorID(baseName string) (string, error) {
	var n abi.SectorNumber
	var mid abi.ActorID
	read, err := fmt.Sscanf(baseName, "s-t0%d-%d", &mid, &n)
	if err != nil {
		return "", xerrors.Errorf("sscanf sector name ('%s'): %w", baseName, err)
	}
	if read != 2 {
		return "", xerrors.Errorf("parseSectorID expected to scan 2 values, got %d", read)
	}
	return baseName, nil
}
func ftFromString(t string) (string, error) {
	switch t {
	case stores.FTUnsealed.String():
	case stores.FTSealed.String():
	case stores.FTCache.String():
	case "all":
		return t, nil
	default:
		return "", xerrors.Errorf("unknown sector file type: '%s'", t)
	}
	return t, nil
}
