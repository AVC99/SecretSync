BINARY  := secretsync
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

# Where `go install` puts the binary: $GOBIN, or the first GOPATH entry's bin.
GOBIN_DIR := $(shell go env GOBIN)
ifeq ($(GOBIN_DIR),)
GOBIN_DIR := $(firstword $(subst :, ,$(shell go env GOPATH)))/bin
endif

.PHONY: build test install dist snapshot clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go vet ./...
	go test ./...

install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" .
	@printf '\n✔ %s installed to %s\n' "$(BINARY)" "$(GOBIN_DIR)"
	@case ":$$PATH:" in \
		*":$(GOBIN_DIR):"*) printf '  Already in your PATH: run "%s".\n' "$(BINARY)" ;; \
		*) printf '  %s is not in your PATH yet.\n' "$(GOBIN_DIR)" ;; \
	esac
	@printf '\nTo run "%s" from any directory:\n' "$(BINARY)"
	@printf '\n  macOS (zsh):\n'
	@printf '    %s\n' "echo 'export PATH=\"\$$PATH:$(GOBIN_DIR)\"' >> ~/.zshrc && source ~/.zshrc"
	@printf '    or with a symlink: %s\n' "sudo ln -sf $(GOBIN_DIR)/$(BINARY) /usr/local/bin/$(BINARY)"
	@printf '\n  Linux (bash):\n'
	@printf '    %s\n' "echo 'export PATH=\"\$$PATH:$(GOBIN_DIR)\"' >> ~/.bashrc && source ~/.bashrc"
	@printf '    or with a symlink: %s\n' "mkdir -p ~/.local/bin && ln -sf $(GOBIN_DIR)/$(BINARY) ~/.local/bin/$(BINARY)"
	@printf '\n  Windows (PowerShell, then open a new terminal):\n'
	@printf '    %s\n' "[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';' + (go env GOPATH) + '\\bin', 'User')"
	@printf '\n'

dist:
	@mkdir -p dist
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=; \
		[ $$os = windows ] && ext=.exe; \
		out=dist/$(BINARY)-$$os-$$arch$$ext; \
		echo "-> $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
	done

# Builds a full release (archives, checksums, Homebrew cask) into dist/
# without publishing anything. Requires goreleaser.
snapshot:
	TAP_DEPLOY_KEY=unused goreleaser release --snapshot --clean

clean:
	rm -rf dist $(BINARY) $(BINARY).exe
