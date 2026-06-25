BINARY = deer
EXAMPLES = examples

GO ?= go
CGO_ENABLED = 1

LDFLAGS = -s -w
BUILDFLAGS = -trimpath

# Detect OS-specific settings
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
  # macOS: use Homebrew's llvm@20 to avoid libc++ version conflicts.
  # The system shell profile may export CPLUS_INCLUDE_PATH / LIBRARY_PATH
  # pointing at a newer LLVM (>=22), whose libc++ headers require Clang 22+.
  # We unset those and let llvm@20's clang use its own bundled libc++.
  LLVM_PREFIX := $(shell brew --prefix llvm@20 2>/dev/null)
  ifeq ($(LLVM_PREFIX),)
    $(error llvm@20 not found. Install with: brew install llvm@20)
  endif
  CC := $(LLVM_PREFIX)/bin/clang
  CXX := $(LLVM_PREFIX)/bin/clang++
  # -Wl,-export_dynamic: export C symbols so the JIT can resolve them at runtime.
  CGO_LDFLAGS := -Wl,-export_dynamic
  # Ensure the system linker (not Android NDK's) is found first.
  CLEAN_PATH := /Library/Developer/CommandLineTools/usr/bin:/usr/bin:/bin:/usr/sbin:/sbin
  ENV := env -u CPLUS_INCLUDE_PATH -u C_INCLUDE_PATH -u LIBRARY_PATH \
    PATH="$(CLEAN_PATH):$(LLVM_PREFIX)/bin:$$PATH" \
    CC="$(CC)" CXX="$(CXX)" \
    CGO_LDFLAGS="$(CGO_LDFLAGS)"
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
		$(GO) build $(BUILDFLAGS) -ldflags="$(LDFLAGS)" -o ./$(BINARY)

test:
	$(GO) vet ./...
	$(ENV) $(GO) build ./...

clean:
	rm -f $(BINARY) *.o *.tok *.ast

# Run all examples in JIT mode
examples: build
	@for f in $(EXAMPLES)/*.deer; do \
		echo "=== $$f ==="; \
		./$(BINARY) -e "$$f" 2>&1; \
	done

run: build
	./$(BINARY) -e $(EXAMPLES)/hello.deer

tok: build
	./$(BINARY) -o /tmp/hello.tok $(EXAMPLES)/hello.deer
	@echo "=== Tokens ==="
	@cat /tmp/hello.tok

ast: build
	./$(BINARY) -o /tmp/hello.ast $(EXAMPLES)/hello.deer
	@echo "=== AST ==="
	@cat /tmp/hello.ast

ir: build
	./$(BINARY) -d $(EXAMPLES)/hello.deer

fmt:
	gofmt -w -s .
