// +build windows

package server

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func init() {
	runningServerReg = sync.RWMutex{}
	runningServers = make(map[string]*EndlessServer)
	runningServersOrder = []string{}
	socketPtrOffsetMap = make(map[string]uint)

	DefaultMaxHeaderBytes = 0 // use http.DefaultMaxHeaderBytes - which currently is 1 << 20 (1MB)

	// after a restart the parent will finish ongoing requests before
	// shutting down. set to a negative value to disable
	DefaultHammerTime = 60 * time.Second

	hookableSignals = []os.Signal{
		os.Interrupt,
		syscall.SIGTERM,
	}

	signalHooks = map[int]map[os.Signal][]func(){
		PRE_SIGNAL: map[os.Signal][]func(){
			os.Interrupt:    []func(){},
			syscall.SIGTERM: []func(){},
		},
		POST_SIGNAL: map[os.Signal][]func(){
			os.Interrupt:    []func(){},
			syscall.SIGTERM: []func(){},
		},
	}

}

/*
handleSignals listens for os Signals and calls any hooked in function that the
user had registered with the signal.
*/
func (srv *EndlessServer) handleSignals() {
	var sig os.Signal

	signal.Notify(
		srv.sigChan,
		hookableSignals...,
	)

	for {
		sig = <-srv.sigChan
		srv.signalHooks(PRE_SIGNAL, sig)
		switch sig {
		case os.Interrupt:
			log.Println("Received Interupt.")
			srv.shutdown()
		case syscall.SIGTERM:
			log.Println("Received SIGTERM.")
			srv.shutdown()

		default:
			log.Printf("Received %v: nothing i care about...\n", sig)
		}
		srv.signalHooks(POST_SIGNAL, sig)
	}
}

func (srv *EndlessServer) ListenAndServe() (err error) {
	addr := srv.Addr
	if addr == "" {
		addr = ":http"
	}

	go srv.handleSignals()

	l, err := srv.getListener(addr)
	if err != nil {
		log.Println(err)
		return
	}

	srv.EndlessListener = newEndlessListener(l, srv)

	srv.BeforeBegin(srv.Addr)

	return srv.Serve()
}

/*
ListenAndServeTLS listens on the TCP network address srv.Addr and then calls
Serve to handle requests on incoming TLS connections.

Filenames containing a certificate and matching private key for the server must
be provided. If the certificate is signed by a certificate authority, the
certFile should be the concatenation of the server's certificate followed by the
CA's certificate.

If srv.Addr is blank, ":https" is used.
*/
func (srv *EndlessServer) ListenAndServeTLS(certFile, keyFile string) (err error) {
	addr := srv.Addr
	if addr == "" {
		addr = ":https"
	}

	config := &tls.Config{}
	if srv.TLSConfig != nil {
		*config = *srv.TLSConfig
	}
	if config.NextProtos == nil {
		config.NextProtos = []string{"http/1.1"}
	}

	config.Certificates = make([]tls.Certificate, 1)
	config.Certificates[0], err = tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return
	}

	go srv.handleSignals()

	l, err := srv.getListener(addr)
	if err != nil {
		log.Println(err)
		return
	}

	srv.tlsInnerListener = newEndlessListener(l, srv)
	srv.EndlessListener = tls.NewListener(srv.tlsInnerListener, config)

	log.Println(syscall.Getpid(), srv.Addr)
	return srv.Serve()
}
