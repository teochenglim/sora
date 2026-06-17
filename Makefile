BINARY      := sora
MODULE      := github.com/teochenglim/sora
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -X 'main.buildVersion=$(VERSION)' -X 'github.com/teochenglim/sora/internal/webhook.Version=$(VERSION)'
REGISTRY    := ghcr.io/teochenglim
IMAGE       := $(REGISTRY)/sora:$(VERSION)
PLATFORMS   := linux/amd64,linux/arm64

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help (default target)
	@echo "SORA — Service Operations Remediation Agent"
	@echo ""
	@echo "Usage: make <target>"
	@echo ""
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## /{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build the sora binary into bin/
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/sora

.PHONY: test
test: ## Run the full test suite with race detector and coverage
	go test ./... -race -cover

.PHONY: test-e2e
test-e2e: ## Black-box HTTP test against an ALREADY-RUNNING sora (start one first: make docker-up / run-demo / run-local)
	SORA_URL=$${SORA_URL:-http://localhost:8080} ./scripts/test-e2e.sh

.PHONY: coverage
coverage: ## Generate an HTML coverage report at coverage.html
	go test ./tests/... -coverpkg=./internal/...,./pkg/... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html in a browser"

.PHONY: lint
lint: ## Run golangci-lint (falls back to go vet if not installed)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found, falling back to go vet"; \
		go vet ./...; \
	fi

.PHONY: security
security: ## Run semgrep + trivy locally (same checks as .github/workflows/security.yml)
	@if command -v semgrep >/dev/null 2>&1; then \
		semgrep scan --config p/golang --config p/secrets --config p/owasp-top-ten --config p/sql-injection --config p/dockerfile --config p/kubernetes --error; \
	else \
		echo "semgrep not installed (pip install semgrep / brew install semgrep), skipping"; \
	fi
	@if command -v trivy >/dev/null 2>&1; then \
		trivy fs --scanners vuln --severity CRITICAL,HIGH .; \
	else \
		echo "trivy not installed (brew install trivy), skipping"; \
	fi

.PHONY: fmt
fmt: ## gofmt all source files
	gofmt -l -w .

.PHONY: tidy
tidy: ## Sync go.mod/go.sum with imports
	go mod tidy

.PHONY: run-demo
run-demo: build ## Run sora in --mode=demo (no external dependencies required)
	./bin/$(BINARY) --mode=demo

.PHONY: run-local
run-local: build ## Run sora with configs/config.yaml
	./bin/$(BINARY) --config=configs/config.yaml

.PHONY: docker-build
docker-build: ## Build the sora Docker image for the local platform
	docker build -f deployments/docker/Dockerfile -t $(IMAGE) .

.PHONY: docker-buildx
docker-buildx: ## Build & push a multi-arch (amd64+arm64) image via buildx
	docker buildx build --platform $(PLATFORMS) -f deployments/docker/Dockerfile -t $(IMAGE) --push .

.PHONY: docker-push
docker-push: ## Push the locally built image
	docker push $(IMAGE)

.PHONY: docker-up
docker-up: ## Start sora + Redis + Prometheus via docker-compose
	docker compose -f deployments/docker/docker-compose.yaml up --build

.PHONY: docker-down
docker-down: ## Stop and remove the docker-compose stack
	docker compose -f deployments/docker/docker-compose.yaml down

.PHONY: docker-logs
docker-logs: ## Tail logs from the docker-compose stack
	docker compose -f deployments/docker/docker-compose.yaml logs -f

.PHONY: release-dry-run
release-dry-run: ## Build release binaries for every platform locally, without tagging or pushing anything
	@mkdir -p dist
	@for platform in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		goos=$${platform%/*}; goarch=$${platform#*/}; \
		echo "building $$goos/$$goarch..."; \
		CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch go build -ldflags "$(LDFLAGS)" -o dist/sora-$$goos-$$goarch ./cmd/sora || exit 1; \
		tar -C dist -czf dist/sora-$$goos-$$goarch.tar.gz sora-$$goos-$$goarch; \
		rm dist/sora-$$goos-$$goarch; \
	done
	@cd dist && sha256sum *.tar.gz > checksums.txt
	@ls -la dist/

.PHONY: release
release: ## Tag and push vX.Y.Z (VERSION=x.y.z required) — triggers .github/workflows/release.yml
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release VERSION=x.y.z"; exit 1; fi
	@if [ -n "$$(git status --porcelain)" ]; then echo "working tree not clean — commit or stash first"; exit 1; fi
	@git fetch -q origin
	@if [ "$$(git rev-parse HEAD)" != "$$(git rev-parse @{u} 2>/dev/null)" ]; then \
		echo "HEAD does not match the remote tracking branch — push your commits first so the tag is bound to code that's actually on GitHub"; \
		exit 1; \
	fi
	$(MAKE) test
	git tag -a "v$(VERSION)" -m "Release v$(VERSION)" "$$(git rev-parse HEAD)"
	git push origin "v$(VERSION)"
	@echo "Tagged $$(git rev-parse --short HEAD) as v$(VERSION) and pushed — https://github.com/teochenglim/sora/actions"

.PHONY: k8s-deploy
k8s-deploy: ## Apply the plain Kubernetes manifests
	kubectl apply -f deployments/k8s/

.PHONY: helm-deploy
helm-deploy: ## Install/upgrade the Helm chart
	helm upgrade --install sora deployments/helm/

.PHONY: hooks-install
hooks-install: ## Install the git pre-commit hook from scripts/pre-commit.sh
	mkdir -p .git/hooks
	cp scripts/pre-commit.sh .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit
	@echo "pre-commit hook installed"

.PHONY: clean
clean: ## Remove build/test artifacts
	rm -rf bin/ coverage.out coverage.html sora-learning.db
