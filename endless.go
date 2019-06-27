package server

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"

	//	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	// "github.com/fvbock/uds-go/introspect"
)

const (
	PRE_SIGNAL = iota
	POST_SIGNAL

	STATE_INIT
	STATE_RUNNING
	STATE_SHUTTING_DOWN
	STATE_TERMINATE
)

var (
	runningServerReg     sync.RWMutex
	runningServers       map[string]*EndlessServer
	runningServersOrder  []string
	socketPtrOffsetMap   map[string]uint
	runningServersForked bool

	DefaultReadTimeOut    time.Duration
	DefaultWriteTimeOut   time.Duration
	DefaultMaxHeaderBytes int
	DefaultHammerTime     time.Duration

	isChild     bool
	socketOrder string

	hookableSignals []os.Signal
)

var signalHooks map[int]map[os.Signal][]func()

type EndlessServer struct {
	http.Server
	EndlessListener  net.Listener
	SignalHooks      map[int]map[os.Signal][]func()
	tlsInnerListener *endlessListener
	wg               sync.WaitGroup
	sigChan          chan os.Signal
	isChild          bool
	state            uint8
	lock             *sync.RWMutex
	BeforeBegin      func(add string)
}

type endlessListener struct {
	net.Listener
	stopped bool
	server  *EndlessServer
}

/*
NewServer returns an intialized endlessServer Object. Calling Serve on it will
actually "start" the server.
*/
func NewServer(addr string, handler http.Handler) (srv *EndlessServer) {
	runningServerReg.Lock()
	defer runningServerReg.Unlock()

	socketOrder = os.Getenv("ENDLESS_SOCKET_ORDER")
	isChild = os.Getenv("ENDLESS_CONTINUE") != ""

	if len(socketOrder) > 0 {
		for i, addr := range strings.Split(socketOrder, ",") {
			socketPtrOffsetMap[addr] = uint(i)
		}
	} else {
		socketPtrOffsetMap[addr] = uint(len(runningServersOrder))
	}

	srv = &EndlessServer{
		wg:          sync.WaitGroup{},
		sigChan:     make(chan os.Signal),
		isChild:     isChild,
		SignalHooks: signalHooks,
		state:       STATE_INIT,
		lock:        &sync.RWMutex{},
	}

	srv.Server.Addr = addr
	srv.Server.ReadTimeout = DefaultReadTimeOut
	srv.Server.WriteTimeout = DefaultWriteTimeOut
	srv.Server.MaxHeaderBytes = DefaultMaxHeaderBytes
	srv.Server.Handler = handler

	srv.BeforeBegin = func(addr string) {
		log.Println(syscall.Getpid(), addr)
	}

	runningServersOrder = append(runningServersOrder, addr)
	runningServers[addr] = srv

	return
}

func ListenAndServe(addr string, handler http.Handler) error {
	server := NewServer(addr, handler)
	return server.ListenAndServe()
}

/*
ListenAndServeTLS acts identically to ListenAndServe, except that it expects
HTTPS connections. Additionally, files containing a certificate and matching
private key for the server must be provided. If the certificate is signed by a
certificate authority, the certFile should be the concatenation of the server's
certificate followed by the CA's certificate.
*/
func ListenAndServeTLS(addr string, certFile string, keyFile string, handler http.Handler) error {
	server := NewServer(addr, handler)
	return server.ListenAndServeTLS(certFile, keyFile)
}

func (srv *EndlessServer) getState() uint8 {
	srv.lock.RLock()
	defer srv.lock.RUnlock()

	return srv.state
}

func (srv *EndlessServer) setState(st uint8) {
	srv.lock.Lock()
	defer srv.lock.Unlock()

	srv.state = st
}

/*
Serve accepts incoming HTTP connections on the listener l, creating a new
service goroutine for each. The service goroutines read requests and then call
handler to reply to them. Handler is typically nil, in which case the
DefaultServeMux is used.

In addition to the stl Serve behaviour each connection is added to a
sync.Waitgroup so that all outstanding connections can be served before shutting
down the server.
*/
func (srv *EndlessServer) Serve() (err error) {
	defer log.Println(syscall.Getpid(), "Serve() returning...")
	srv.setState(STATE_RUNNING)
	err = srv.Server.Serve(srv.EndlessListener)
	log.Println(syscall.Getpid(), "Waiting for connections to finish...")
	srv.wg.Wait()
	srv.setState(STATE_TERMINATE)
	return
}

/*
getListener either opens a new socket to listen on, or takes the acceptor socket
it got passed when restarted.
*/
func (srv *EndlessServer) getListener(laddr string) (l net.Listener, err error) {
	if srv.isChild {
		var ptrOffset uint = 0
		runningServerReg.RLock()
		defer runningServerReg.RUnlock()
		if len(socketPtrOffsetMap) > 0 {
			ptrOffset = socketPtrOffsetMap[laddr]
			// log.Println("laddr", laddr, "ptr offset", socketPtrOffsetMap[laddr])
		}

		f := os.NewFile(uintptr(3+ptrOffset), "")
		//f := os.NewFile(uintptr(3), "")

		l, err = net.FileListener(f)
		if err != nil {
			err = fmt.Errorf("net.FileListener error: %v", err)
			return
		}
	} else {
		l, err = net.Listen("tcp", laddr)
		if err != nil {
			err = fmt.Errorf("net.Listen error: %v", err)
			return
		}
	}
	return
}

func (srv *EndlessServer) signalHooks(ppFlag int, sig os.Signal) {
	if _, notSet := srv.SignalHooks[ppFlag][sig]; !notSet {
		return
	}
	for _, f := range srv.SignalHooks[ppFlag][sig] {
		f()
	}
	return
}

/*
shutdown closes the listener so that no new connections are accepted. it also
starts a goroutine that will hammer (stop all running requests) the server
after DefaultHammerTime.
*/
func (srv *EndlessServer) shutdown() {
	if srv.getState() != STATE_RUNNING {
		return
	}

	srv.setState(STATE_SHUTTING_DOWN)
	if DefaultHammerTime >= 0 {
		go srv.hammerTime(DefaultHammerTime)
	}
	// disable keep-alives on existing connections
	srv.SetKeepAlivesEnabled(false)
	err := srv.EndlessListener.Close()
	if err != nil {
		log.Println(syscall.Getpid(), "Listener.Close() error:", err)
	} else {
		log.Println(syscall.Getpid(), srv.EndlessListener.Addr(), "Listener closed.")
	}
}

/*
hammerTime forces the server to shutdown in a given timeout - whether it
finished outstanding requests or not. if Read/WriteTimeout are not set or the
max header size is very big a connection could hang...

srv.Serve() will not return until all connections are served. this will
unblock the srv.wg.Wait() in Serve() thus causing ListenAndServe(TLS) to
return.
*/
func (srv *EndlessServer) hammerTime(d time.Duration) {
	defer func() {
		// we are calling srv.wg.Done() until it panics which means we called
		// Done() when the counter was already at 0 and we're done.
		// (and thus Serve() will return and the parent will exit)
		if r := recover(); r != nil {
			log.Println("WaitGroup at 0", r)
		}
	}()
	if srv.getState() != STATE_SHUTTING_DOWN {
		return
	}
	time.Sleep(d)
	log.Println("[STOP - Hammer Time] Forcefully shutting down parent")
	for {
		if srv.getState() == STATE_TERMINATE {
			break
		}
		srv.wg.Done()
		runtime.Gosched()
	}
}

func (srv *EndlessServer) fork() (err error) {
	runningServerReg.Lock()
	defer runningServerReg.Unlock()

	// only one server instance should fork!
	if runningServersForked {
		return errors.New("Another process already forked. Ignoring this one.")
	}

	runningServersForked = true

	var files = make([]*os.File, len(runningServers))
	var orderArgs = make([]string, len(runningServers))
	// get the accessor socket fds for _all_ server instances
	for _, srvPtr := range runningServers {
		// introspect.PrintTypeDump(srvPtr.EndlessListener)
		switch srvPtr.EndlessListener.(type) {
		case *endlessListener:
			// normal listener
			files[socketPtrOffsetMap[srvPtr.Server.Addr]] = srvPtr.EndlessListener.(*endlessListener).File()
		default:
			// tls listener
			files[socketPtrOffsetMap[srvPtr.Server.Addr]] = srvPtr.tlsInnerListener.File()
		}
		orderArgs[socketPtrOffsetMap[srvPtr.Server.Addr]] = srvPtr.Server.Addr
	}

	env := append(
		os.Environ(),
		"ENDLESS_CONTINUE=1",
	)
	if len(runningServers) > 1 {
		env = append(env, fmt.Sprintf(`ENDLESS_SOCKET_ORDER=%s`, strings.Join(orderArgs, ",")))
	}

	// log.Println(files)
	path := os.Args[0]
	var args []string
	if len(os.Args) > 1 {
		args = os.Args[1:]
	}

	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = files
	cmd.Env = env

	err = cmd.Start()
	if err != nil {
		log.Fatalf("Restart: Failed to launch, error: %v", err)
	}

	return
}

func (el *endlessListener) Accept() (c net.Conn, err error) {
	tc, err := el.Listener.(*net.TCPListener).AcceptTCP()
	if err != nil {
		return
	}

	tc.SetKeepAlive(true)                  // see http.tcpKeepAliveListener
	tc.SetKeepAlivePeriod(3 * time.Minute) // see http.tcpKeepAliveListener

	c = endlessConn{
		Conn:   tc,
		server: el.server,
	}

	el.server.wg.Add(1)
	return
}

func newEndlessListener(l net.Listener, srv *EndlessServer) (el *endlessListener) {
	el = &endlessListener{
		Listener: l,
		server:   srv,
	}

	return
}

func (el *endlessListener) Close() error {
	if el.stopped {
		return syscall.EINVAL
	}

	el.stopped = true
	return el.Listener.Close()
}

func (el *endlessListener) File() *os.File {
	// returns a dup(2) - FD_CLOEXEC flag *not* set
	tl := el.Listener.(*net.TCPListener)
	fl, _ := tl.File()
	return fl
}

type endlessConn struct {
	net.Conn
	server *EndlessServer
}

func (w endlessConn) Close() error {
	err := w.Conn.Close()
	if err == nil {
		w.server.wg.Done()
	}
	return err
}

/*
RegisterSignalHook registers a function to be run PRE_SIGNAL or POST_SIGNAL for
a given signal. PRE or POST in this case means before or after the signal
related code endless itself runs
*/
func (srv *EndlessServer) RegisterSignalHook(prePost int, sig os.Signal, f func()) (err error) {
	if prePost != PRE_SIGNAL && prePost != POST_SIGNAL {
		err = fmt.Errorf("Cannot use %v for prePost arg. Must be endless.PRE_SIGNAL or endless.POST_SIGNAL.", sig)
		return
	}
	for _, s := range hookableSignals {
		if s == sig {
			srv.SignalHooks[prePost][sig] = append(srv.SignalHooks[prePost][sig], f)
			return
		}
	}
	err = fmt.Errorf("Signal %v is not supported.", sig)
	return
}
