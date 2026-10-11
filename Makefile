BIN    := matterbase
BINDIR ?= $(HOME)/bin
PREFIX ?= /usr/local

.PHONY: build install uninstall link unlink test clean

build:
	go build -o $(BIN) ./cmd/matterbase

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
	rm -f $(BIN)
