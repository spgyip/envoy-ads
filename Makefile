.PHONY: apis debug release image default

VERSION=$(shell git describe --always --dirty)

GENGODIR="./apis/gengo"

default: debug

apis:
	@test -d ${GENGODIR} || mkdir -p ${GENGODIR}
	protoc  -I ./deps/envoy/api/ \
        -I ./deps/udpa/ \
        -I ./deps/protoc-gen-validate/ \
        --go_out=${GENGODIR} \
        --go_opt=paths=source_relative \
        --go_opt=Mudpa/annotations/versioning.proto=github.com/cncf/xds/go/udpa/annotations \
        --go_opt=Mudpa/annotations/migrate.proto=github.com/cncf/xds/go/udpa/annotations \
        --go_opt=Mudpa/annotations/status.proto=github.com/cncf/xds/go/udpa/annotations \
        --go_opt=Mxds/core/v3/context_params.proto="github.com/cncf/go/xds/core/v3;xdscorev3" \
        --go-grpc_out=${GENGODIR} \
        --go-grpc_opt=paths=source_relative \
        --go-grpc_opt=Mudpa/annotations/versioning.proto=github.com/cncf/xds/go/udpa/annotations \
        --go-grpc_opt=Mudpa/annotations/migrate.proto=github.com/cncf/xds/go/udpa/annotations \
        --go-grpc_opt=Mudpa/annotations/status.proto=github.com/cncf/xds/go/udpa/annotations \
        --go-grpc_opt=Mxds/core/v3/context_params.proto="github.com/cncf/go/xds/core/v3;xdscorev3" \
        deps/envoy/api/envoy/service/discovery/v3/ads.proto \
        deps/envoy/api/envoy/service/discovery/v3/discovery.proto

debug:
	go build -ldflags="-X main.version=${VERSION} -X main.build=debug" -o bin/ ./cmd/*

release:
	go build -ldflags="-X main.version=${VERSION} -X main.build=release" -o bin/ ./cmd/*

image:
	sudo docker build -t supergui/envoy-ads:$(VERSION) -f docker/Dockerfile .

clean:
	@rm -fv bin/*
