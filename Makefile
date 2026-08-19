.PHONY: build test fmt vet

# A tagged checkout gets the nearest semantic Git description. Repositories
# without a reachable tag leave this empty so Go's embedded VCS revision and
# dirty state remain the authoritative development identity.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null)
LDFLAGS := $(if $(VERSION),-X github.com/merefield/codexometer/internal/version.buildVersion=$(VERSION),)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o codexometer .

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...
