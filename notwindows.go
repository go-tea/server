// +build !windows

package server

import (
	"log"
	"os"
	"os/signal"
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
		syscall.SIGHUP,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGTSTP,
	}

	signalHooks = map[int]map[os.Signal][]func(){
		PRE_SIGNAL: map[os.Signal][]func(){
			syscall.SIGHUP:  []func(){},
			syscall.SIGUSR1: []func(){},
			syscall.SIGUSR2: []func(){},
			syscall.SIGINT:  []func(){},
			syscall.SIGTERM: []func(){},
			syscall.SIGTSTP: []func(){},
		},
		POST_SIGNAL: map[os.Signal][]func(){
			syscall.SIGHUP:  []func(){},
			syscall.SIGUSR1: []func(){},
			syscall.SIGUSR2: []func(){},
			syscall.SIGINT:  []func(){},
			syscall.SIGTERM: []func(){},
			syscall.SIGTSTP: []func(){},
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

	pid := syscall.Getpid()
	for {
		sig = <-srv.sigChan
		srv.signalHooks(PRE_SIGNAL, sig)
		switch sig {
		case syscall.SIGHUP:
			log.Println(pid, "Received SIGHUP. forking.")
			err := srv.fork()
			if err != nil {
				log.Println("Fork err:", err)
			}
		case syscall.SIGUSR1:
			log.Println(pid, "Received SIGUSR1.")
		case syscall.SIGUSR2:
			log.Println(pid, "Received SIGUSR2.")
			srv.hammerTime(0 * time.Second)
		case syscall.SIGINT:
			log.Println(pid, "Received SIGINT.")
			srv.shutdown()
		case syscall.SIGTERM:
			log.Println(pid, "Received SIGTERM.")
			srv.shutdown()
		case syscall.SIGTSTP:
			log.Println(pid, "Received SIGTSTP.")
		default:
			log.Printf("Received %v: nothing i care about...\n", sig)
		}
		srv.signalHooks(POST_SIGNAL, sig)
	}
}

func (srv *EndlessServer) ischild() {
	if srv.isChild {
		syscall.Kill(syscall.Getppid(), syscall.SIGTERM)
	}
}
