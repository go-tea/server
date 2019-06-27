VERSION = 0.0.1

all: clean build

GO_BIN = go

clean:
	rm -rf GOPATH/build/tea/server-$(VERSION)_linux-64



build:
#	GOOS=linux GOARCH=amd64 go build -v github.com/go-tea/server
	GOOS=windows GOARCH=amd64 go build -o $(GOPATH)/pkg/windows_amd64/github.com/go-tea/server.a github.com/go-tea/server


	
	
