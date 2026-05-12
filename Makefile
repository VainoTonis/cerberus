VERSION    := $(shell git describe --tags --always --dirty)
LDFLAGS    := -ldflags "-X main.version=$(VERSION)"
BINARY     := cerberus
INSTALL    := $(HOME)/.local/bin/$(BINARY)
CONFIG_DIR := $(HOME)/.config/cerberus
CONFIG     := $(CONFIG_DIR)/config.json

.PHONY: build install init-config clean

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/cerberus

init-config:
	@mkdir -p $(CONFIG_DIR)
	@if [ ! -f $(CONFIG) ]; then \
		cp config.example.json $(CONFIG); \
		echo "created $(CONFIG)"; \
	else \
		echo "$(CONFIG) already exists, skipping"; \
	fi

install: build init-config
	cp $(BINARY) $(INSTALL)
	@echo "installed $(INSTALL) $(VERSION)"

clean:
	rm -f $(BINARY)
