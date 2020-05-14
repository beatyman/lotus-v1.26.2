package fileserver

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/gorilla/mux"
	logging "github.com/ipfs/go-log/v2"
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
