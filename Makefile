
.PHONY: all
all: swag


.PHONY: swag
swag: swagfmt swaginit


.PHONY: swagfmt
swagfmt:
	echo "▶️  Formatting Swagger files"
	go tool swag fmt -g internal/http/swagger_apis/swagger_apis.go

.PHONY: swaginit
swaginit:
	echo "▶️  Initializing Swagger files"
	go tool swag init \
		-g internal/http/swagger_apis/swagger_apis.go \
		--outputTypes "yaml" \
		--output "api_specs" --instanceName=all_apis \
		--tags="Auctions"
