# server

Fork from endless with modification for windows

[![GoDoc](https://godoc.org/github.com/fvbock/endless?status.svg)](https://godoc.org/github.com/fvbock/endless)


## Signals

The server will listen for the following signals: 
- Linux:
`syscall.SIGHUP`, `syscall.SIGUSR1`, `syscall.SIGUSR2`, `syscall.SIGINT`, `syscall.SIGTERM`, and `syscall.SIGTSTP`

- Windows:
  `syscall.SIGTERM`