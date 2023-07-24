SHELL=/usr/bin/env bash
.DEFAULT_GOAL := build

FILECOIN_FFI_VERSION=de34caff946d598e
FILECOIN_FFI_HOME=extern/filecoin-ffi

.PHONY: build
build: extern/filecoin-ffi
	go build ./...

.PHONY: clean
clean:
	rm -rf ./$(FILECOIN_FFI_HOME)

extern/filecoin-ffi:
	git clone --depth 1 https://github.com/filecoin-project/filecoin-ffi.git $(FILECOIN_FFI_HOME) && \
	cd $(FILECOIN_FFI_HOME) && \
	git fetch origin $(FILECOIN_FFI_VERSION) && \
	git checkout $(FILECOIN_FFI_VERSION) && \
	$(MAKE)

