// +build windows

package server

import (
	"log"
	"os"
	"os/signal"
	"sync"

	"syscall"
	"time"
	//"github.com/golang/sys/windows"
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

func (srv *EndlessServer) ischild() {
	if srv.isChild {
		ppid := syscall.Getppid()
		process, err := os.FindProcess(ppid)
		if err != nil {
			return
		}
		process.Kill()
	}

}
