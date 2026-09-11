.PHONY: build test integration-test check fmt vet release-snapshot web-build web-test

# A tagged checkout gets the nearest semantic Git description. Repositories
# without a reachable tag leave this empty so Go's embedded VCS revision and
# dirty state remain the authoritative development identity.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null)
LDFLAGS := $(if $(VERSION),-X github.com/merefield/codexometer/internal/version.buildVersion=$(VERSION),)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o codexometer .

test:
	go test ./...

integration-test:
	bats test/install-release.bats

check: vet test integration-test

fmt:
	go fmt ./...

vet:
	go vet ./...

release-snapshot:
	goreleaser release --snapshot --clean --skip=publish

# Only frontend contributors need Node. Generated assets are committed for
# ordinary Go builds, go install and all six GoReleaser distributions.
web-build:
	cd web && npm ci && npm run check && npm run build

web-test: build
	cd web && npm test
