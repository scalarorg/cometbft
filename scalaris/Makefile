PACKAGES=$(shell go list ./...)
BUILDDIR?=$(CURDIR)/build
OUTPUT?=$(BUILDDIR)/scalaris

HTTPS_GIT := https://github.com/cometbft/cometbft.git
CGO_ENABLED ?= 0

# Process Docker environment variable TARGETPLATFORM
# in order to build binary with correspondent ARCH
# by default will always build for linux/amd64
TARGETPLATFORM ?=
GOOS ?= linux
GOARCH ?= amd64
GOARM ?=

ifeq (linux/arm,$(findstring linux/arm,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=arm
	GOARM=7
endif

ifeq (linux/arm/v6,$(findstring linux/arm/v6,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=arm
	GOARM=6
endif

ifeq (linux/arm64,$(findstring linux/arm64,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=arm64
	GOARM=7
endif

ifeq (linux/386,$(findstring linux/386,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=386
endif

ifeq (linux/amd64,$(findstring linux/amd64,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=amd64
endif

ifeq (linux/mips,$(findstring linux/mips,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=mips
endif

ifeq (linux/mipsle,$(findstring linux/mipsle,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=mipsle
endif

ifeq (linux/mips64,$(findstring linux/mips64,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=mips64
endif

ifeq (linux/mips64le,$(findstring linux/mips64le,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=mips64le
endif

ifeq (linux/riscv64,$(findstring linux/riscv64,$(TARGETPLATFORM)))
	GOOS=linux
	GOARCH=riscv64
endif

###############################################################################
###                                Protobuf                                 ###
###############################################################################

#? check-proto-deps: Check protobuf deps
check-proto-deps:
ifeq (,$(shell which protoc-gen-gocosmos))
	@go install github.com/scalarorg/common@dev
	@go install github.com/cosmos/gogoproto/protoc-gen-gocosmos@latest
endif
.PHONY: check-proto-deps

#? check-proto-format-deps: Check protobuf format deps
check-proto-format-deps:
ifeq (,$(shell which clang-format))
	$(error "clang-format is required for Protobuf formatting. See instructions for your platform on how to install it.")
endif
.PHONY: check-proto-format-deps

install-protoc:
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@go install github.com/bufbuild/buf/cmd/buf@latest
.PHONY:install-protoc

#? proto-gen: Generate protobuf files
# proto-gen: check-proto-deps
# proto-gen:
# 	@echo "Generating Protobuf files"
# 	@go run github.com/bufbuild/buf/cmd/buf generate
# 	# @mv ./proto/tendermint/abci/types.pb.go ./abci/types/
# 	# @cp ./proto/tendermint/rpc/grpc/types.pb.go ./rpc/grpc
# .PHONY: proto-gen

proto-gen:
	@echo "Generating Protobuf files"
	@protoc --go_out=./consensus/ --go_opt=paths=source_relative \
    		--go-grpc_out=./consensus --go-grpc_opt=paths=source_relative \
			proto/types.proto proto/service.proto 
#	@go run github.com/bufbuild/buf/cmd/buf@latest generate --path proto/consensus
.PHONY: proto-gen



# These targets are provided for convenience and are intended for local
# execution only.
#? proto-lint: Lint protobuf files
proto-lint: check-proto-deps
	@echo "Linting Protobuf files"
	@go run github.com/bufbuild/buf/cmd/buf@latest lint
.PHONY: proto-lint

#? proto-format: Format protobuf files
proto-format: check-proto-format-deps
	@echo "Formatting Protobuf files"
	@find . -name '*.proto' -path "./proto/*" -exec clang-format -i {} \;
.PHONY: proto-format

#? proto-check-breaking: Check for breaking changes in Protobuf files against local branch. This is only useful if your changes have not yet been committed
proto-check-breaking: check-proto-deps
	@echo "Checking for breaking changes in Protobuf files against local branch"
	@echo "Note: This is only useful if your changes have not yet been committed."
	@echo "      Otherwise read up on buf's \"breaking\" command usage:"
	@echo "      https://docs.buf.build/breaking/usage"
	@go run github.com/bufbuild/buf/cmd/buf@latest breaking --against ".git"
.PHONY: proto-check-breaking

#? proto-check-breaking-ci: Check for breaking changes in Protobuf files against v0.34.x. This is only useful if your changes have not yet been committed
proto-check-breaking-ci:
	@go run github.com/bufbuild/buf/cmd/buf@latest breaking --against $(HTTPS_GIT)#branch=v0.34.x
.PHONY: proto-check-breaking-ci

###############################################################################
###                                Scalaris                                 ###
###############################################################################

#? build_scalaris: Build scalaris
build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) CGO_ENABLED=$(CGO_ENABLED) go build $(BUILD_FLAGS) -tags '$(BUILD_TAGS)' -o $(OUTPUT) ./cmd/
.PHONY: build

#? install_scalaris: Install scalaris
install:
	GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) CGO_ENABLED=$(CGO_ENABLED) go install $(BUILD_FLAGS) -tags '$(BUILD_TAGS)' -o $(OUTPUT) ./cmd/
.PHONY: build

#? docker-build: Build docker image scalaris/abci
docker-build: build
	docker build \
		--label=scalaris \
		--tag="scalaris/abci" \
		-f Dockerfile .
.PHONY: build-docker

docker-start: docker-stop docker-build
	docker-compose -f docker-compose.yml up -d
.PHONY: docker-start

docker-stop:
	docker-compose -f docker-compose.yml down
	docker rmi scalaris/abci
.PHONY: docker-stop