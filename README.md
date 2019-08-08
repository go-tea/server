
[![GoDoc](http://img.shields.io/badge/go-documentation-blue.svg?style=flat-square)](http://godoc.org/github.com/go-tea/server) 
[![Go Report Card](https://goreportcard.com/badge/github.com/go-tea/server)](https://goreportcard.com/report/github.com/go-tea/server)

# server

Fork from endless with modification for windows

## Signals

The server will listen for the following signals: 
- Linux:
`syscall.SIGHUP`, `syscall.SIGUSR1`, `syscall.SIGUSR2`, `syscall.SIGINT`, `syscall.SIGTERM`, and `syscall.SIGTSTP`

- Windows:
  `syscall.SIGTERM`