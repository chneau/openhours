.SILENT:
.ONESHELL:
.NOTPARALLEL:
.EXPORT_ALL_VARIABLES:
.PHONY: run test deps dev

name=$(shell basename $(CURDIR))

run: test

test:
	TZ=Europe/London go test -v -count=1 ./...

deps:
	govendor init
	govendor add +e
	govendor update +v

dev:
	go get -u -v github.com/kardianos/govendor
