package fileserver

import (
	"encoding/xml"
	"net/http"
	"os"
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
