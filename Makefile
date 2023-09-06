SHELL=/usr/bin/env bash
.DEFAULT_GOAL := build

.PHONY: build
build:
	go build ./...

.PHONY: clean
clean:
