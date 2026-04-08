VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
BINARY  := cerberus
INSTALL := $(HOME)/.local/bin/$(BINARY)

.PHONY: build install clean

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/cerberus

install:
	go build $(LDFLAGS) -o $(INSTALL) ./cmd/cerberus
	@echo "installed $(INSTALL) $(VERSION)"

clean:
	rm -f $(BINARY)
