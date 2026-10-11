BIN    := matterbase
BINDIR ?= $(HOME)/bin
PREFIX ?= /usr/local

# The version comes from the tag. A dirty or untagged tree says so in
# `matterbase --version`.
VERSION := $(shell git describe --tags --dirty --always 2>/dev/null | sed 's/^v//')
LDFLAGS := -X main.version=$(VERSION)
RELEASE := CGO_ENABLED=0 go build -trimpath -ldflags="-s -w $(LDFLAGS)"

.PHONY: build install uninstall link unlink test release clean

build:
	go build -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/matterbase

release:
	GOOS=darwin GOARCH=arm64 $(RELEASE) -o $(BIN)-macos-arm64 ./cmd/matterbase
	GOOS=darwin GOARCH=amd64 $(RELEASE) -o $(BIN)-macos-amd64 ./cmd/matterbase
	GOOS=linux GOARCH=amd64 $(RELEASE) -o $(BIN)-linux-amd64 ./cmd/matterbase
	GOOS=linux GOARCH=arm64 $(RELEASE) -o $(BIN)-linux-arm64 ./cmd/matterbase

# Link once, then never again: rebuilding is deploying. A stale copy fails
# silently, a dangling link fails at the next call. Convention: ../BUILD.md.
link: build
	@install -d $(BINDIR)
	@ln -sfn $(CURDIR)/$(BIN) $(BINDIR)/$(BIN)
	@echo "linked $(BINDIR)/$(BIN) -> $(CURDIR)/$(BIN)"

unlink:
	rm -f $(BINDIR)/$(BIN)

# `install` copies into $(PREFIX)/bin, for anyone who clones this. A clone is
# not a stable place to point a symlink at — the symlink form is `make link`,
# for the machine this is developed on.
install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/$(BIN)

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

test:
	go test -race ./...

clean:
	rm -f $(BIN) $(BIN)-macos-arm64 $(BIN)-macos-amd64 $(BIN)-linux-amd64 $(BIN)-linux-arm64
