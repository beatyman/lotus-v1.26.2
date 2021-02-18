package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	promclient "github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"contrib.go.opencensus.io/exporter/prometheus"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/apistruct"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/gwaylib/errors"
)

var log = logging.Logger("main")

func createTLSCert() error {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, _ := rand.Int(rand.Reader, max)
	subject := pkix.Name{
		Organization:       []string{"Fivestar"},
		OrganizationalUnit: []string{"lotus"},
		CommonName:         "fivestar-lotus",
	}

	rootTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      subject,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return errors.As(err)
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &rootTemplate, &rootTemplate, &pk.PublicKey, pk)
	if err != nil {
		return errors.As(err)
	}

	certOut, err := os.Create("/etc/lotus/lotus.crt")
	if err != nil {
		return errors.As(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return errors.As(err)
	}

	certOut.Close()

	keyOut, err := os.Create("/etc/lotus/lotus.key")
	if err != nil {
		return errors.As(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(pk)}); err != nil {
		return errors.As(err)
	}
	keyOut.Close()
	return nil
}

func serveRPC(a api.FullNode, stop node.StopFunc, addr multiaddr.Multiaddr, shutdownCh <-chan struct{}, maxRequestSize int64) error {
	serverOptions := make([]jsonrpc.ServerOption, 0)
	if maxRequestSize != 0 { // config set
		serverOptions = append(serverOptions, jsonrpc.WithMaxRequestSize(maxRequestSize))
	}
	rpcServer := jsonrpc.NewServer(serverOptions...)
	rpcServer.Register("Filecoin", apistruct.PermissionedFullAPI(metrics.MetricedFullAPI(a)))

	ah := &auth.Handler{
		Verify: a.AuthVerify,
		Next:   rpcServer.ServeHTTP,
	}

	http.Handle("/rpc/v0", ah)

	importAH := &auth.Handler{
		Verify: a.AuthVerify,
		Next:   handleImport(a.(*impl.FullNodeAPI)),
	}

	http.Handle("/rest/v0/import", importAH)

	// Prometheus globals are exposed as interfaces, but the prometheus
	// OpenCensus exporter expects a concrete *Registry. The concrete type of
	// the globals are actually *Registry, so we downcast them, staying
	// defensive in case things change under the hood.
	registry, ok := promclient.DefaultRegisterer.(*promclient.Registry)
	if !ok {
		log.Warnf("failed to export default prometheus registry; some metrics will be unavailable; unexpected type: %T", promclient.DefaultRegisterer)
	}
	exporter, err := prometheus.NewExporter(prometheus.Options{
		Registry:  registry,
		Namespace: "lotus",
	})
	if err != nil {
		log.Fatalf("could not create the prometheus stats exporter: %v", err)
	}

	http.Handle("/debug/metrics", exporter)
	http.Handle("/debug/pprof-set/block", handleFractionOpt("BlockProfileRate", runtime.SetBlockProfileRate))
	http.Handle("/debug/pprof-set/mutex", handleFractionOpt("MutexProfileFraction",
		func(x int) { runtime.SetMutexProfileFraction(x) },
	))

	lst, err := manet.Listen(addr)
	if err != nil {
		return xerrors.Errorf("could not listen: %w", err)
	}

	srv := &http.Server{
		Handler: http.DefaultServeMux,
		BaseContext: func(listener net.Listener) context.Context {
			ctx, _ := tag.New(context.Background(), tag.Upsert(metrics.APIInterface, "lotus-daemon"))
			return ctx
		},
	}

	sigCh := make(chan os.Signal, 2)
	shutdownDone := make(chan struct{})
	go func() {
		select {
		case sig := <-sigCh:
			log.Warnw("received shutdown", "signal", sig)
		case <-shutdownCh:
			log.Warn("received shutdown")
		}

		log.Warn("Shutting down...")
		if err := srv.Shutdown(context.TODO()); err != nil {
			log.Errorf("shutting down RPC server failed: %s", err)
		}
		if err := stop(context.TODO()); err != nil {
			log.Errorf("graceful shutting down failed: %s", err)
		}
		log.Warn("Graceful shutdown successful")
		_ = log.Sync() //nolint:errcheck
		close(shutdownDone)
	}()
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	if _, err := os.Stat("/etc/lotus/lotus.crt"); err != nil {
		log.Error(errors.As(err))
		log.Warn("building tls cert automatic")
		if err := os.MkdirAll("/etc/lotus", 0755); err != nil {
			return errors.As(err)
		}
		if err := createTLSCert(); err != nil {
			return errors.As(err)
		}
	}

	err = srv.ServeTLS(manet.NetListener(lst), "/etc/lotus/lotus.crt", "/etc/lotus/lotus.key")
	if err == http.ErrServerClosed {
		<-shutdownDone
		return nil
	}
	return err
}

func handleImport(a *impl.FullNodeAPI) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			w.WriteHeader(404)
			return
		}
		if !auth.HasPerm(r.Context(), nil, apistruct.PermWrite) {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(struct{ Error string }{"unauthorized: missing write permission"})
			return
		}

		c, err := a.ClientImportLocal(r.Context(), r.Body)
		if err != nil {
			w.WriteHeader(500)
			_ = json.NewEncoder(w).Encode(struct{ Error string }{err.Error()})
			return
		}
		w.WriteHeader(200)
		err = json.NewEncoder(w).Encode(struct{ Cid cid.Cid }{c})
		if err != nil {
			log.Errorf("/rest/v0/import: Writing response failed: %+v", err)
			return
		}
	}
}
