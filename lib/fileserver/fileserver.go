package fileserver

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"

	"path/filepath"
	"sync"

	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api/apistruct"
	"github.com/filecoin-project/sector-storage/stores"
	"github.com/filecoin-project/sector-storage/tarutil"
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

type FileHandle struct {
	Repo string
}

func (f *fileHandle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	addConns(1)
	defer addConns(-1)
	f.handler.ServeHTTP(w, r)
}

func NewStorageFileServer(repo, token string, next http.Handler) *StorageFileServer {
	r := mux.NewRouter()
	r.PathPrefix("/filecoin-proof-parameters").Handler(http.StripPrefix(
		"/filecoin-proof-parameters",
		&fileHandle{handler: http.FileServer(http.Dir("/var/tmp/filecoin-proof-parameters"))},
	))

	r.PathPrefix("/storage/cache").Handler(http.StripPrefix(
		"/storage/cache",
		&fileHandle{handler: http.FileServer(http.Dir(filepath.Join(repo, "cache")))},
	))
	r.PathPrefix("/storage/unsealed").Handler(http.StripPrefix(
		"/storage/unsealed",
		&fileHandle{handler: http.FileServer(http.Dir(filepath.Join(repo, "unsealed")))},
	))
	r.PathPrefix("/storage/sealed").Handler(http.StripPrefix(
		"/storage/sealed",
		&fileHandle{handler: http.FileServer(http.Dir(filepath.Join(repo, "sealed")))},
	))

	r.HandleFunc("/storage/delete", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("flags", "done")
		// TODO: make auth
		sid := r.FormValue("sid")

		if len(sid) == 0 {
			w.WriteHeader(404)
			w.Write([]byte("sector id not found"))
			return
		}
		log.Infof("try delete cache:%s", sid)
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
	})

	return &StorageFileServer{
		repo:   repo,
		token:  token,
		router: r,
		next:   next,
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

func (s *StorageFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: make auth
	fmt.Printf("1:%+v\n", w.Header())
	s.router.ServeHTTP(w, r)
	fmt.Printf("2:%+v\n", w.Header())
	//if len(w.Header()) > 0 {
	//	return
	//}

	if s.next != nil {
		s.next.ServeHTTP(w, r)
	}
}

func (f *FileHandle) FileHttpServer(w http.ResponseWriter, r *http.Request) {
	// auth
	//fmt.Println("testing fileServer")
	if !auth.HasPerm(r.Context(), nil, apistruct.PermAdmin) {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(struct{ Error string }{"unauthorized: missing write permission"})
		return
	}

	mu := mux.NewRouter()
	//test
	mu.PathPrefix("/file/filecoin-proof-parameters").Handler(http.StripPrefix(
		"/file/filecoin-proof-parameters",
		&fileHandle{handler: http.FileServer(http.Dir("/var/tmp/filecoin-proof-parameters"))},
	))

	mu.PathPrefix("/file/storage/cache").Handler(http.StripPrefix(
		"/file/storage/cache",
		&fileHandle{handler: http.FileServer(http.Dir(filepath.Join(f.Repo, "cache")))},
	))
	mu.PathPrefix("/file/storage/unsealed").Handler(http.StripPrefix(
		"/file/storage/unsealed",
		&fileHandle{handler: http.FileServer(http.Dir(filepath.Join(f.Repo, "unsealed")))},
	))
	mu.PathPrefix("/file/storage/sealed").Handler(http.StripPrefix(
		"/file/storage/sealed",
		&fileHandle{handler: http.FileServer(http.Dir(filepath.Join(f.Repo, "sealed")))},
	))

	mu.HandleFunc("/file/storage/delete", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("flags", "done")
		// TODO: make auth
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
			if err := os.RemoveAll(filepath.Join(f.Repo, "cache", sid)); err != nil {
				w.WriteHeader(500)
				w.Write([]byte("delete cache failed:" + err.Error()))
				return
			}
			if err := os.RemoveAll(filepath.Join(f.Repo, "sealed", sid)); err != nil {
				w.WriteHeader(500)
				w.Write([]byte("delete sealed failed:" + err.Error()))
				return
			}
			if err := os.RemoveAll(filepath.Join(f.Repo, "unsealed", sid)); err != nil {
				w.WriteHeader(500)
				w.Write([]byte("delete unsealed failed:" + err.Error()))
				return
			}
		} else {
			path := filepath.Join(f.Repo, ft, sid)
			if err := os.RemoveAll(path); err != nil {
				w.WriteHeader(500)
				w.Write([]byte("delete cache failed:" + err.Error()))
				return
			}
		}
	})

	mu.ServeHTTP(w, r)
}

func (f *FileHandle) fileProofParameters(w http.ResponseWriter, r *http.Request) {
	fmt.Println("proofParameters fileServer")
	path := "/var/tmp/filecoin-proof-parameters"
	stat, err := os.Stat(path)
	if err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}

	var rd io.Reader
	if stat.IsDir() {
		rd, err = tarutil.TarDirectory(path)
		w.Header().Set("Content-Type", "application/x-tar")
	} else {
		rd, err = os.OpenFile(path, os.O_RDONLY, 0644)
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}

	w.WriteHeader(200)
	if _, err := io.Copy(w, rd); err != nil { // TODO: default 32k buf may be too small
		log.Error("%+v", err)
		return
	}
}

func (f *FileHandle) fileGetSector(w http.ResponseWriter, r *http.Request) {
	log.Infof("SERVE GET %s", r.URL)
	vars := mux.Vars(r)

	id, err := parseSectorID(vars["id"])
	if err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}

	ft, err := ftFromString(vars["type"])
	if err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}

	path := filepath.Join(f.Repo, ft, id)
	fmt.Println("path :", path)
	stat, err := os.Stat(path)
	if err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}

	var rd io.Reader
	if stat.IsDir() {
		rd, err = tarutil.TarDirectory(path)
		w.Header().Set("Content-Type", "application/x-tar")
	} else {
		rd, err = os.OpenFile(path, os.O_RDONLY, 0644)
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}

	w.WriteHeader(200)
	if _, err := io.Copy(w, rd); err != nil { // TODO: default 32k buf may be too small
		log.Error("%+v", err)
		return
	}
}

func (f *FileHandle) fileDeleteSector(w http.ResponseWriter, r *http.Request) {
	log.Infof("SERVE DELETE %s", r.URL)
	vars := mux.Vars(r)

	sid, err := parseSectorID(vars["id"])
	if err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}

	ft, err := ftFromString(vars["type"])
	if err != nil {
		log.Error("%+v", err)
		w.WriteHeader(500)
		return
	}
	if ft == "all" {
		if err := os.RemoveAll(filepath.Join(f.Repo, "cache", sid)); err != nil {
			w.WriteHeader(500)
			w.Write([]byte("delete cache failed:" + err.Error()))
			return
		}
		if err := os.RemoveAll(filepath.Join(f.Repo, "sealed", sid)); err != nil {
			w.WriteHeader(500)
			w.Write([]byte("delete sealed failed:" + err.Error()))
			return
		}
		if err := os.RemoveAll(filepath.Join(f.Repo, "unsealed", sid)); err != nil {
			w.WriteHeader(500)
			w.Write([]byte("delete unsealed failed:" + err.Error()))
			return
		}
	} else {
		path := filepath.Join(f.Repo, ft, sid)
		fmt.Println("path :", path)
		if err := os.RemoveAll(path); err != nil {
			w.WriteHeader(500)
			w.Write([]byte(fmt.Sprint("delete %s failed:", ft) + err.Error()))
		}
	}
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
func fileTest(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	json.NewEncoder(w).Encode(struct{ Error string }{"unauthorized: fileTest"})
	return
}
