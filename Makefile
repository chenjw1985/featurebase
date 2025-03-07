.PHONY: build check-clean clean build-lattice cover cover-viz default docker docker-build docker-test docker-tag-push generate generate-protoc generate-pql generate-statik gometalinter install install-build-deps install-golangci-lint install-gometalinter install-protoc install-protoc-gen-gofast install-peg install-statik release release-build test testv testv-race testvsub testvsub-race test-txstore-rbf

CLONE_URL=github.com/pilosa/pilosa
VERSION := $(shell git describe --tags 2> /dev/null || echo unknown)
VARIANT = Molecula
GO=go
GOOS=$(shell $(GO) env GOOS)
GOARCH=$(shell $(GO) env GOARCH)
VERSION_ID=$(if $(TRIAL_DEADLINE),trial-$(TRIAL_DEADLINE)-,)$(VERSION)-$(GOOS)-$(GOARCH)
BRANCH := $(if $(CIRCLE_BRANCH),$(CIRCLE_BRANCH),$(shell git rev-parse --abbrev-ref HEAD))
BRANCH_ID := $(BRANCH)-$(GOOS)-$(GOARCH)
BUILD_TIME := $(shell date -u +%FT%T%z)
SHARD_WIDTH = 20
COMMIT := $(shell git describe --exact-match >/dev/null 2>&1 || git rev-parse --short HEAD)
LDFLAGS="-X github.com/featurebasedb/featurebase/v3.Version=$(VERSION) -X github.com/featurebasedb/featurebase/v3.BuildTime=$(BUILD_TIME) -X github.com/featurebasedb/featurebase/v3.Variant=$(VARIANT) -X github.com/featurebasedb/featurebase/v3.Commit=$(COMMIT) -X github.com/featurebasedb/featurebase/v3.TrialDeadline=$(TRIAL_DEADLINE)"
GO_VERSION=1.19
DOCKER_BUILD= # set to 1 to use `docker-build` instead of `build` when creating a release
BUILD_TAGS += shardwidth$(SHARD_WIDTH)
TEST_TAGS = roaringparanoia
UNAME := $(shell uname -s)
TEST_TIMEOUT=10m
RACE_TEST_TIMEOUT=10m
ifeq ($(UNAME), Darwin)
    IS_MACOS:=1
else
    IS_MACOS:=0
endif

export GO111MODULE=on
export GOPRIVATE=github.com/molecula
export CGO_ENABLED=0

# Run tests and compile Pilosa
default: test build

# Remove build directories
clean:
	rm -rf vendor build
	rm -f *.rpm *.deb
	
# Set up vendor directory using `go mod vendor`
vendor: go.mod
	$(GO) mod vendor

version:
	@echo $(VERSION)

# We build a list of packages that omits the IDK packages because the IDK
# packages require fancy environment setup.
GOPACKAGES := $(shell $(GO) list ./... | grep -v "/idk")

# Run test suite
test:
	$(GO) test $(GOPACKAGES) -tags='$(BUILD_TAGS) $(TEST_TAGS)' $(TESTFLAGS) -v -timeout $(TEST_TIMEOUT)

# Run test suite with race flag
test-race:
	CGO_ENABLED=1 $(GO) test $(GOPACKAGES) -tags='$(BUILD_TAGS) $(TEST_TAGS)' $(TESTFLAGS) -race -timeout $(RACE_TEST_TIMEOUT) -v

testv: testvsub

testv-race: testvsub-race

# testvsub: run go test -v in sub-directories in "local mode" with incremental output,
#            avoiding go -test ./... "package list mode" which doesn't give output
#            until the test run finishes. Package list mode makes it hard to
#            find which test is hung/deadlocked.
#
testvsub:
	@set -e; for pkg in $(GOPACKAGES); do \
			if [ $${pkg:0:38} == "github.com/featurebasedb/featurebase/v3/idk" ]; then \
				echo; echo "___ skipping subpkg $$pkg"; \
				continue; \
			fi; \
			echo; echo "___ testing subpkg $$pkg"; \
			$(GO) test -tags='$(BUILD_TAGS) $(TEST_TAGS)' $(TESTFLAGS) -v -timeout $(RACE_TEST_TIMEOUT) $$pkg || break; \
			echo; echo "999 done testing subpkg $$pkg"; \
		done


testvsub-race:
	@set -e; for pkg in $(GOPACKAGES); do \
           echo; echo "___ testing subpkg $$pkg"; \
           CGO_ENABLED=1 $(GO) test -tags='$(BUILD_TAGS) $(TEST_TAGS)' $(TESTFLAGS) -v -race -timeout $(RACE_TEST_TIMEOUT) $$pkg || break; \
           echo; echo "999 done testing subpkg $$pkg"; \
        done

bench:
	$(GO) test $(GOPACKAGES) -bench=. -run=NoneZ -timeout=127m $(TESTFLAGS)

# Run test suite with coverage enabled
cover:
	mkdir -p build
	$(MAKE) test TESTFLAGS="-coverprofile=build/coverage.out"

# Run test suite with coverage enabled and view coverage results in browser
cover-viz: cover
	$(GO) tool cover -html=build/coverage.out

# Compile Pilosa
build:
	$(GO) build -tags='$(BUILD_TAGS)' -ldflags $(LDFLAGS) $(FLAGS) ./cmd/featurebase

# Create a single release build under the build directory
release-build:
	$(MAKE) $(if $(DOCKER_BUILD),docker-)build FLAGS="-o build/featurebase-$(VERSION_ID)/featurebase"
	cp NOTICE install/featurebase.conf install/featurebase*.service build/featurebase-$(VERSION_ID)
	tar -cvz -C build -f build/featurebase-$(VERSION_ID).tar.gz featurebase-$(VERSION_ID)/
	@echo Created release build: build/featurebase-$(VERSION_ID).tar.gz

test-release-build: docker-build
	mv build/featurebase-$(VERSION_ID).tar.gz install/
	cd install && docker build -t featurebase:test_installation \
		-f test_installation.Dockerfile \
		--build-arg release_tarball=featurebase-$(VERSION_ID).tar.gz .
	mv install/featurebase-$(VERSION_ID).tar.gz build/
	docker run -it -v /sys/fs/cgroup:/sys/fs/cgroup:ro \
		featurebase:test_installation

# Error out if there are untracked changes in Git
check-clean:
ifndef SKIP_CHECK_CLEAN
	$(if $(shell git status --porcelain),$(error Git status is not clean! Please commit or checkout/reset changes.))
endif

# Create release build tarballs for all supported platforms. DEPRECATED: Use `docker-release`
release: check-clean generate-statik-docker
	$(MAKE) release-build GOOS=darwin GOARCH=amd64
	$(MAKE) release-build GOOS=darwin GOARCH=arm64
	$(MAKE) release-build GOOS=linux GOARCH=amd64
	$(MAKE) release-build GOOS=linux GOARCH=arm64

# Create release build tarballs for all supported platforms. Same as `release`, but without embedded Lattice UI.
release-sans-ui: check-clean
	rm -f statik/statik.go
	$(MAKE) release-build GOOS=darwin GOARCH=amd64
	$(MAKE) release-build GOOS=darwin GOARCH=arm64
	$(MAKE) release-build GOOS=linux GOARCH=amd64
	$(MAKE) release-build GOOS=linux GOARCH=arm64

package:
	go build -o featurebase ./cmd/featurebase
	GOARCH=$(GOARCH) VERSION=$(VERSION) nfpm package --packager deb --target featurebase.$(VERSION).$(GOARCH).deb
	GOARCH=$(GOARCH) VERSION=$(VERSION) nfpm package --packager rpm --target featurebase.$(VERSION).$(GOARCH).rpm
	
# We allow setting a custom docker-compose "project". Multiple of the
# same docker-compose environment can exist simultaneously as long as
# they use different projects (the project name is prepended to
# container names and such). This is useful in a CI environment where
# we might be running multiple instances of the tests concurrently.
PROJECT ?= clustertests
DOCKER_COMPOSE = docker-compose -p $(PROJECT)

# Run cluster integration tests using docker. Requires docker daemon to be
# running and docker-compose to be installed.
clustertests: vendor
	$(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml down
	$(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml build
	$(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml up -d pilosa1 pilosa2 pilosa3
	PROJECT=$(PROJECT) $(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml run client1
	$(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml down

# Run the cluster tests with authentication enabled
AUTH_ARGS="-c /go/src/github.com/featurebasedb/featurebase/internal/clustertests/testdata/featurebase.conf"
authclustertests: vendor
	CLUSTERTESTS_FB_ARGS=$(AUTH_ARGS) $(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml down
	CLUSTERTESTS_FB_ARGS=$(AUTH_ARGS) $(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml build
	CLUSTERTESTS_FB_ARGS=$(AUTH_ARGS) $(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml up -d pilosa1 pilosa2 pilosa3
	PROJECT=$(PROJECT) ENABLE_AUTH=1 $(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml run client1
	CLUSTERTESTS_FB_ARGS=$(AUTH_ARGS) $(DOCKER_COMPOSE) -f internal/clustertests/docker-compose.yml down

# Install Pilosa
install:
	$(GO) install -tags='$(BUILD_TAGS)' -ldflags $(LDFLAGS) $(FLAGS) ./cmd/featurebase

# Install the single-node PLG version of FeatureBase
plg:
	$(GO) build -tags='plg $(BUILD_TAGS)' -ldflags $(LDFLAGS) $(FLAGS) ./cmd/featurebase

install-bench:
	$(GO) install -tags='$(BUILD_TAGS)' -ldflags $(LDFLAGS) $(FLAGS) ./cmd/pilosa-bench

# Build the lattice assets
build-lattice:
	docker build -t lattice:build ./lattice
	export LATTICE=`docker create lattice:build`; docker cp $$LATTICE:/lattice/. ./lattice/build && docker rm $$LATTICE

# `go generate` protocol buffers
generate-protoc: require-protoc require-protoc-gen-gofast
	$(GO) generate github.com/featurebasedb/featurebase/v3/pb

# `go generate` statik assets (lattice UI)
generate-statik: build-lattice require-statik
	$(GO) generate github.com/featurebasedb/featurebase/v3/statik

# `go generate` statik assets (lattice UI) in Docker
generate-statik-docker: build-lattice
	docker run --rm -t -v $(PWD):/pilosa golang:1.15.8 sh -c "go get github.com/rakyll/statik && /go/bin/statik -src=/pilosa/lattice/build -dest=/pilosa -f"

# `go generate` stringers
generate-stringer:
	$(GO) generate github.com/featurebasedb/featurebase/v3

generate-pql: require-peg
	cd pql && peg -inline pql.peg && cd ..

generate-proto-grpc: require-protoc require-protoc-gen-go
	protoc -I proto proto/pilosa.proto --go_out=plugins=grpc:proto
	protoc -I proto proto/vdsm/vdsm.proto --go_out=plugins=grpc:proto
	# TODO: Modify above commands and remove the below mv if possible.
	# See https://go-review.googlesource.com/c/protobuf/+/219298/ for info on --go-opt
	# I couldn't get it to work during development - Cody
	cp -r proto/github.com/featurebasedb/featurebase/v3/proto/ proto/
	rm -rf proto/github.com

# `go generate` all needed packages
generate: generate-protoc generate-statik generate-stringer generate-pql

# Create release using Docker
docker-release:
	$(MAKE) docker-build GOOS=linux GOARCH=amd64
	$(MAKE) docker-build GOOS=linux GOARCH=arm64
	$(MAKE) docker-build GOOS=darwin GOARCH=amd64
	$(MAKE) docker-build GOOS=darwin GOARCH=arm64

# Build a release in Docker
docker-build: vendor
	docker build \
	    --build-arg GO_VERSION=$(GO_VERSION) \
	    --build-arg MAKE_FLAGS="TRIAL_DEADLINE=$(TRIAL_DEADLINE) GOOS=$(GOOS) GOARCH=$(GOARCH)" \
	    --target pilosa-builder \
	    --tag featurebase:build .
	docker create --name featurebase-build featurebase:build
	mkdir -p build/featurebase-$(VERSION_ID)
	docker cp featurebase-build:/pilosa/build/. ./build/featurebase-$(VERSION_ID)
	cp NOTICE install/featurebase.conf install/featurebase*.service ./build/featurebase-$(VERSION_ID)
	docker rm featurebase-build
	tar -cvz -C build -f build/featurebase-$(VERSION_ID).tar.gz featurebase-$(VERSION_ID)/

# Create Docker image from Dockerfile
docker-image: vendor
	docker build \
	    --build-arg GO_VERSION=$(GO_VERSION) \
	    --build-arg MAKE_FLAGS="TRIAL_DEADLINE=$(TRIAL_DEADLINE)" \
	    --tag featurebase:$(VERSION) .
	@echo Created docker image: featurebase:$(VERSION)

# Create docker image (alias)
docker: docker-image # alias

# Tag and push a Docker image
docker-tag-push: vendor
	docker tag "featurebase:$(VERSION)" $(DOCKER_TARGET)
	docker push $(DOCKER_TARGET)
	@echo Pushed docker image: $(DOCKER_TARGET)

# These commands (docker-idk and docker-idk-tag-push)
# are designed to be used in CI.
# docker-idk builds idk docker images and tags them - intended for use in CI.
docker-idk: vendor
	docker build \
		-f idk/Dockerfile \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg MAKE_FLAGS="GOOS=$(GOOS) GOARCH=$(GOARCH) BUILD_CGO=$(BUILD_CGO)" \
		--tag registry.gitlab.com/molecula/featurebase/idk:$(VERSION_ID) .
	@echo Created docker image: registry.gitlab.com/molecula/featurebase/idk:$(VERSION_ID)
# docker-idk-tag-push pushes tagged docker images to the GitLab container
# registry - intended for use in CI.
docker-idk-tag-push:
	docker push registry.gitlab.com/molecula/featurebase/idk:$(VERSION_ID)
	@echo Pushed docker image: registry.gitlab.com/molecula/featurebase/idk:$(VERSION_ID)

# Install diagnostic pilosa-keydump tool. Allows viewing the keys in a transaction-engine directory.
pilosa-keydump:
	$(GO) install -tags='$(BUILD_TAGS)' -ldflags $(LDFLAGS) $(FLAGS) ./cmd/pilosa-keydump

# Install diagnostic pilosa-chk tool for string translations and fragment checksums.
pilosa-chk:
	$(GO) install -tags='$(BUILD_TAGS)' -ldflags $(LDFLAGS) $(FLAGS) ./cmd/pilosa-chk

pilosa-fsck:
	cd ./cmd/pilosa-fsck && make install && make release

# Run Pilosa tests inside Docker container
docker-test:
	docker run --rm -v $(PWD):/go/src/$(CLONE_URL) -w /go/src/$(CLONE_URL) golang:$(GO_VERSION) go test -tags='$(BUILD_TAGS) $(TEST_TAGS)' $(TESTFLAGS) -timeout $(TEST_TIMEOUT) $(GOPACKAGES)

# Must use bash in order to -o pipefail; otherwise the tee will hide red tests.
# run top tests, not subdirs. print summary red/green after.
# The \-\-\- FAIL avoids counting the extra two FAIL strings at then bottom of log.topt.
topt:
	mv log.topt.roar log.topt.roar.prev || true
	$(eval SHELL:=/bin/bash) set -o pipefail; $(GO) test -v -timeout $(RACE_TEST_TIMEOUT) -tags='$(BUILD_TAGS) $(TEST_TAGS)' $(TESTFLAGS) 2>&1 | tee log.topt.roar
	@echo "   log.topt.roar green: \c"; cat log.topt.roar | grep PASS |wc -l
	@echo "   log.topt.roar   red: \c"; cat log.topt.roar | grep '\-\-\- FAIL' | wc -l

topt-race:
	mv log.topt.race log.topt.race.prev || true
	$(eval SHELL:=/bin/bash) set -o pipefail; CGO_ENABLED=1 $(GO) test -race -timeout $(RACE_TEST_TIMEOUT) -v -tags='$(BUILD_TAGS) $(TEST_TAGS)' $(TESTFLAGS) 2>&1 | tee log.topt.race
	@echo "   log.topt.race green: \c"; cat log.topt.race | grep PASS |wc -l
	@echo "   log.topt.race   red: \c"; cat log.topt.race | grep '\-\-\- FAIL' | wc -l

# Run golangci-lint
golangci-lint: require-golangci-lint
	golangci-lint run --timeout 3m --skip-files '.*\.peg\.go'

# Alias
linter: golangci-lint

# Better alias
ocd: golangci-lint

# Run gometalinter with custom flags
# Note the "./..." in gometalinter is still allowed, because we do want
# linting to reach IDK pagkages.
gometalinter: require-gometalinter vendor
	GO111MODULE=off gometalinter --vendor --disable-all \
	    --deadline=300s \
	    --enable=deadcode \
	    --enable=gochecknoinits \
	    --enable=gofmt \
	    --enable=goimports \
	    --enable=gotype \
	    --enable=gotypex \
	    --enable=ineffassign \
	    --enable=interfacer \
	    --enable=maligned \
	    --enable=misspell \
	    --enable=nakedret \
	    --enable=staticcheck \
	    --enable=unconvert \
	    --enable=unparam \
	    --enable=vet \
	    --exclude "^internal/.*\.pb\.go" \
	    --exclude "^pql/pql.peg.go" \
	    ./...

######################
# Build dependencies #
######################

# Verifies that needed build dependency is installed. Errors out if not installed.
require-%:
	$(if $(shell command -v $* 2>/dev/null),\
		$(info Verified build dependency "$*" is installed.),\
		$(error Build dependency "$*" not installed. To install, try `make install-$*`))

install-build-deps: install-protoc-gen-gofast install-protoc install-statik install-stringer install-peg

install-statik:
	go install github.com/rakyll/statik@latest

install-stringer:
	GO111MODULE=off $(GO) get -u golang.org/x/tools/cmd/stringer

install-protoc-gen-gofast:
	GO111MODULE=off $(GO) get -u github.com/gogo/protobuf/protoc-gen-gofast

install-protoc-gen-go:
	GO111MODULE=off $(GO) get -u github.com/golang/protobuf/protoc-gen-go

install-protoc:
	@echo This tool cannot automatically install protoc. Please download and install protoc from https://google.github.io/proto-lens/installing-protoc.html
	@echo On mac, brew install protobuf seems to work.
	@echo As of the commit that added this line, protoc-gen-gofast was at 226206f39bd7, and the protoc version in use was:
	@echo $$ protoc --version
	@echo libprotoc 3.19.4

install-peg:
	GO111MODULE=off $(GO) get github.com/pointlander/peg

install-golangci-lint:
	GO111MODULE=off $(GO) get github.com/golangci/golangci-lint/cmd/golangci-lint

install-gometalinter:
	GO111MODULE=off $(GO) get -u github.com/alecthomas/gometalinter
	GO111MODULE=off gometalinter --install
	GO111MODULE=off $(GO) get github.com/remyoudompheng/go-misc/deadcode

test-external-lookup:
	$(GO) test . -tags='$(BUILD_TAGS) $(TEST_TAGS)' $(TESTFLAGS) -run ^TestExternalLookup$$ -externalLookupDSN $(EXTERNAL_LOOKUP_DSN)

bnf:
	ebnf2railroad --no-overview-diagram  --no-optimizations ./sql3/sql3.ebnf
