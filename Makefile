SHELL := /bin/bash

VERSION := 0.0.1
RELEASE_DIR := release
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
BINARY := $(RELEASE_DIR)/$(GOOS)/$(GOARCH)/modelserver
MAIN := cmd/modelserver/main.go
LDFLAGS := -ldflags "-X github.com/replicate/replicate/go/pkg/global.Version=$(VERSION) -w"
DOCKER_TAG := "us-central1-docker.pkg.dev/replicate/andreas-scratch/modelserver:$(VERSION)"

.PHONY: build
build: clean
	@mkdir -p $(RELEASE_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) $(MAIN)

.PHONY: clean
clean:
	rm -rf $(RELEASE_DIR)

.PHONY: deploy
deploy: clean
	gcloud builds submit --tag "$(DOCKER_TAG)"
	gcloud run deploy modelserver --region us-central1 --image "$(DOCKER_TAG)" --platform managed --allow-unauthenticated --args=server --service-account=modelstorage@replicate.iam.gserviceaccount.com --timeout=10min

.PHONY: save-db-password
save-db-password:
	# before running this, write the password to db-password.txt
	gcloud secrets describe modelserver-db-password || gcloud secrets create modelserver-db-password --replication-policy="automatic"
	gcloud secrets versions add modelserver-db-password --data-file="db-password.txt"
	rm db-password.txt
