.PHONY: proto test
proto:
	protoc -I proto --go_out=services/gateway/gen/shieldv1 --go_opt=paths=source_relative --go-grpc_out=services/gateway/gen/shieldv1 --go-grpc_opt=paths=source_relative proto/shield.proto
test:
	cd services/engine && cargo test
	cd services/gateway && go test ./...
	cd apps/web-console && npm test && npm run build
