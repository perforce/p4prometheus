BINARY=p4proxyprometheus

VERSION=`git describe --tags`
BUILD_DATE=`date +%FT%T%z`
USER=`git config user.email`
BRANCH=`git rev-parse --abbrev-ref HEAD`
REVISION=`git rev-parse --short HEAD`

MODULE="github.com/perforce/p4prometheus"
LDFLAGS=-ldflags "-w -s -X ${MODULE}/version.Version=${VERSION} -X ${MODULE}/version.BuildDate=${BUILD_DATE} -X ${MODULE}/version.Branch=${BRANCH} -X ${MODULE}/version.Revision=${REVISION} -X ${MODULE}/version.BuildUser=${USER}"

build:
	go build ${LDFLAGS}

dist:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o bin/${BINARY}.linux-amd64 .
	GOOS=linux GOARCH=arm64 go build ${LDFLAGS} -o bin/${BINARY}.linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o bin/${BINARY}.darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build ${LDFLAGS} -o bin/${BINARY}.darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build ${LDFLAGS} -o bin/${BINARY}.windows-amd64.exe .
	gzip -f bin/${BINARY}.linux-amd64 bin/${BINARY}.linux-arm64 bin/${BINARY}.darwin-amd64 bin/${BINARY}.darwin-arm64 bin/${BINARY}.windows-amd64.exe

test:
	go test ./...

.PHONY: build dist test
