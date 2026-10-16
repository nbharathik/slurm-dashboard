MODULE  := github.com/nbharathik/slurm-dashboard
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/internal/meta.Version=$(VERSION) \
	-X $(MODULE)/internal/meta.Commit=$(COMMIT) \
	-X $(MODULE)/internal/meta.Date=$(DATE) \
	-X $(MODULE)/internal/meta.Build=make
GOLANGCI_LINT ?= golangci-lint
PREFIX  ?= $(HOME)/.local
BINDIR  := $(PREFIX)/bin
COMPDIR := $(PREFIX)/share/bash-completion/completions

.PHONY: build install uninstall update path-hint test vet lint fmt check cross clean cluster-up cluster-seed cluster-shell cluster-down compat

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o sdash ./cmd/sdash

install: build
	@mkdir -p "$(BINDIR)" "$(COMPDIR)"
	cp sdash "$(BINDIR)/.sdash.new" && chmod 0755 "$(BINDIR)/.sdash.new" && mv -f "$(BINDIR)/.sdash.new" "$(BINDIR)/sdash"
	./sdash completion bash > "$(COMPDIR)/sdash"
	@echo "Installed $(BINDIR)/sdash"
	@$(MAKE) --no-print-directory path-hint

uninstall:
	rm -f "$(BINDIR)/sdash" "$(COMPDIR)/sdash"

update:
	git pull --ff-only
	$(MAKE) build install

path-hint:
	@case ":$$PATH:" in *":$(BINDIR):"*) ;; \
	*) echo "$(BINDIR) is not on your PATH. Add this line to ~/.bashrc:"; \
	   echo "  export PATH=\"$(BINDIR):\$$PATH\"" ;; esac

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run ./...
	$(GOLANGCI_LINT) run --build-tags integration ./internal/integration/ ./internal/ui/

fmt:
	$(GOLANGCI_LINT) fmt ./...

check: vet lint test

clean:
	rm -rf sdash dist completions

TARGETS := linux/amd64 linux/arm64 darwin/arm64

cross:
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/sdash_$${os}_$${arch} ./cmd/sdash || exit 1; \
	done

SLURM ?= 24.11
SLURM_BASE := $(shell awk '$$1 == "$(SLURM)" { print $$2 }' test/cluster/matrix.txt)
CLUSTER = SLURM=$(SLURM) BASE=$(SLURM_BASE) SDASH_ROOT=$(CURDIR) docker compose -p sdash-$(subst .,-,$(SLURM)) -f test/cluster/compose.yaml

cluster-up:
	mkdir -p build/compat
	CGO_ENABLED=0 go build -trimpath -o build/sdash ./cmd/sdash
	$(CLUSTER) build login
	$(CLUSTER) up -d

cluster-seed:
	$(CLUSTER) exec -T login /opt/sdash/seed.sh

cluster-shell:
	$(CLUSTER) exec -u you -e HOME=/home/you -e USER=you login bash -l

cluster-down:
	$(CLUSTER) down -v --remove-orphans

compat:
	@rc=0; for v in $$(awk '{ print $$1 }' test/cluster/matrix.txt); do bash test/cluster/compat.sh $$v || rc=1; done; \
		python3 test/cluster/report.py build/compat || rc=1; exit $$rc
