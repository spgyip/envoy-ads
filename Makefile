.PHONY: apis

apis:
	protoc  -I ./deps/envoy/api/ \
		    -I ./deps/udpa/ \
		    -I ./deps/protoc-gen-validate/ \
		    --go_out=./apis/ \
			--go_opt=paths=source_relative \
            --go_opt=Mudpa/annotations/status.proto=github.com/cncf/udpa/udpa/annotations \
			--go_opt=Mudpa/annotations/versioning.proto=github.com/cncf/udpa/udpa/annotations \
			--go_opt=Mudpa/annotations/migrate.proto=github.com/cncf/udpa/udpa/annotations \
			--go_opt=Mudpa/annotations/status.proto=github.com/cncf/udpa/udpa/annotations \
			--go_opt=Mxds/core/v3/context_params.proto="github.com/cncf/udpa/xds/core/v3;xdscorev3" \
		    --go-grpc_out=./apis/ \
			--go-grpc_opt=paths=source_relative \
            --go-grpc_opt=Mudpa/annotations/versioning.proto=github.com/cncf/udpa/udpa/annotations \
			--go-grpc_opt=Mudpa/annotations/migrate.proto=github.com/cncf/udpa/udpa/annotations \
			--go-grpc_opt=Mudpa/annotations/status.proto=github.com/cncf/udpa/udpa/annotations \
			--go-grpc_opt=Mxds/core/v3/context_params.proto="github.com/cncf/udpa/xds/core/v3;xdscorev3" \
		    deps/envoy/api/envoy/service/discovery/v3/ads.proto
