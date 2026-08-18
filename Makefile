BIN     := nekoi_mcp
VERSION := 1.0.0
DIST    := dist
WINDRES ?= windres

# Windows carries an icon and a version block; the other platforms have no
# equivalent, so the resource object is built only for the windows target and
# is named *_windows_amd64.syso, which the Go toolchain links exclusively into
# that build.
RSRC := cmd/rsrc_windows_amd64.syso

LDFLAGS := -s -w

.PHONY: all windows linux darwin darwin-arm64 clean check dist

all: check windows linux darwin darwin-arm64

check:
	go vet ./...

$(RSRC): cmd/$(BIN).rc cmd/$(BIN).ico
	$(WINDRES) -i cmd/$(BIN).rc -O coff -o $(RSRC)

windows: $(RSRC)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/windows-amd64/$(BIN).exe ./cmd

# The resource object must be absent for non-Windows builds: the Go toolchain
# only honours the platform suffix, and a stale file confuses nothing, but the
# linker on other platforms has no use for it either way.
linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/linux-amd64/$(BIN) ./cmd

darwin:
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/darwin-amd64/$(BIN) ./cmd

darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/darwin-arm64/$(BIN) ./cmd

dist: all
	@echo "built:"
	@find $(DIST) -type f

clean:
	rm -rf $(DIST) $(RSRC)
