BINARY = deer
EXAMPLES = examples

GO ?= go
CGO_ENABLED = 1

LDFLAGS = -s -w
BUILDFLAGS = -trimpath
# llvm21: select tinygo.org/x/go-llvm's llvm@21 config (per-version cgo
# flags). Mirrored in `go env GOFLAGS` so gopls picks the same config.
GOTAGS = llvm21

# Detect OS-specific settings
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
  # macOS: use llvm@21 — the highest version supported by
  # tinygo.org/x/go-llvm (its per-version config files top out at 21).
  # Using the matching Clang 21 keeps compiler and libc++ headers in sync.
  LLVM_PREFIX := $(shell brew --prefix llvm@21 2>/dev/null)
  ifeq ($(LLVM_PREFIX),)
    $(error llvm@21 not found. Install with: brew install llvm@21)
  endif
  CC := $(LLVM_PREFIX)/bin/clang
  CXX := $(LLVM_PREFIX)/bin/clang++
  # -Wl,-export_dynamic: export C symbols so the JIT can resolve them at runtime.
  CGO_LDFLAGS := -Wl,-export_dynamic
  # Match libLLVM.dylib's minimum OS (minos) to silence the deployment-target
  # mismatch warning from ld.
  MACOSX_DEPLOYMENT_TARGET := $(shell otool -l $(LLVM_PREFIX)/lib/libLLVM.dylib 2>/dev/null | awk '/minos/ {print $$2; exit}')
  ifeq ($(MACOSX_DEPLOYMENT_TARGET),)
    MACOSX_DEPLOYMENT_TARGET := 26.0
  endif
  # Ensure the system linker (not Android NDK's) is found first. Also unset
  # CPLUS_INCLUDE_PATH / C_INCLUDE_PATH / LIBRARY_PATH in case the shell
  # profile points them at a different LLVM version.
  CLEAN_PATH := /Library/Developer/CommandLineTools/usr/bin:/usr/bin:/bin:/usr/sbin:/sbin
  ENV := env -u CPLUS_INCLUDE_PATH -u C_INCLUDE_PATH -u LIBRARY_PATH \
    PATH="$(CLEAN_PATH):$(LLVM_PREFIX)/bin:$$PATH" \
    CC="$(CC)" CXX="$(CXX)" \
    CGO_LDFLAGS="$(CGO_LDFLAGS)" \
    MACOSX_DEPLOYMENT_TARGET="$(MACOSX_DEPLOYMENT_TARGET)"
else
  # Linux: rely on system clang/llvm-config.
  CC ?= clang
  CXX ?= clang++
  ENV := env CC="$(CC)" CXX="$(CXX)"
endif

.PHONY: all build clean test run examples tok ast ir fmt

all: build

build:
	$(ENV) CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build $(BUILDFLAGS) -tags=$(GOTAGS) -ldflags="$(LDFLAGS)" -o ./$(BINARY)

test:
	# -unsafeptr=false: exec.go legitimately converts a JIT C function address
	# (uint64 from LLVM) to unsafe.Pointer via uintptr; this is not a Go pointer
	# and cannot be relocated by the GC. See exec.go for details.
	$(ENV) $(GO) vet -tags=$(GOTAGS) -unsafeptr=false ./...
	$(ENV) $(GO) test -tags=$(GOTAGS) ./...

clean:
	rm -f $(BINARY) *.o *.tok *.ast

# Run all examples in JIT mode
examples: build
	@for f in $(EXAMPLES)/*.dr; do \
		echo "=== $$f ==="; \
		./$(BINARY) -e "$$f" 2>&1; \
	done

run: build
	./$(BINARY) -e $(EXAMPLES)/hello.dr

tok: build
	./$(BINARY) -o /tmp/hello.tok $(EXAMPLES)/hello.dr
	@echo "=== Tokens ==="
	@cat /tmp/hello.tok

ast: build
	./$(BINARY) -o /tmp/hello.ast $(EXAMPLES)/hello.dr
	@echo "=== AST ==="
	@cat /tmp/hello.ast

ir: build
	./$(BINARY) -d $(EXAMPLES)/hello.dr

fmt:
	gofmt -w -s .
