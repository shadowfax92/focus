BINARY := focus
GOBIN ?= $(shell go env GOPATH)/bin
APP_DIR := $(HOME)/Applications/Focus.app
PLIST := $(HOME)/Library/LaunchAgents/com.focus.daemon.plist

.PHONY: build test install reinstall uninstall clean

build:
	go build -o $(BINARY) .

test:
	go test ./...
	go vet ./...

install: build
	GOBIN=$(GOBIN) ./$(BINARY) install

reinstall: build
	-GOBIN=$(GOBIN) ./$(BINARY) uninstall
	GOBIN=$(GOBIN) ./$(BINARY) install

uninstall:
	@if [ -x ./$(BINARY) ]; then GOBIN=$(GOBIN) ./$(BINARY) uninstall; \
	elif [ -x "$(APP_DIR)/Contents/MacOS/$(BINARY)" ]; then GOBIN=$(GOBIN) "$(APP_DIR)/Contents/MacOS/$(BINARY)" uninstall; \
	else echo "focus is not installed"; fi

clean:
	rm -f $(BINARY)
