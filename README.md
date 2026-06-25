Deer Lang
=========

Go port of [LLVM's Kaleidoscope Tutorial](http://llvm.org/docs/tutorial/LangImpl1.html) using the [tinygo](https://github.com/tinygo-org/tinygo) LLVM bindings.

Deer is a simple functional language demonstrating JIT compilation and native code generation via LLVM.

Features
--------
- LLVM-based JIT execution and native code compilation
- User-defined unary/binary operators
- Mutable variables with `var`/`in` syntax
- Control flow: `if`/`then`/`else`, `for` loops
- S-expression style AST dumping for debugging
- Precise source locations (file:line:col) in error messages
- Structured logging via `log/slog`

Prerequisites
-------------
- LLVM 20+ development libraries
- Clang (for linking final executables)
- Go 1.21+

### Ubuntu/Debian
```bash
wget https://apt.llvm.org/llvm.sh
chmod +x llvm.sh
sudo ./llvm.sh 20
sudo apt install clang-20 liblld-20-dev
```

### macOS
```bash
brew install llvm@20
```

> **Note for macOS users:** If you have multiple LLVM versions installed (e.g.,
> `llvm` and `llvm@20`), your shell profile may export `CPLUS_INCLUDE_PATH`
> pointing at the newer LLVM's libc++ headers. These headers require Clang 22+
> and will cause compilation errors with Clang 20. The Makefile automatically
> unsets `CPLUS_INCLUDE_PATH`, `C_INCLUDE_PATH`, and `LIBRARY_PATH` on macOS
> so that `llvm@20`'s bundled libc++ is used.

Building
--------
```bash
make build
```

The Makefile handles OS-specific configuration:
- **macOS**: Uses Homebrew's `llvm@20` clang/clang++, unsets conflicting
  environment variables, and adds `-Wl,-export_dynamic` so the JIT can
  resolve host C functions at runtime.
- **Linux**: Uses system `clang`/`clang++`.

Usage
-----
```bash
# JIT execute a program
./deer -e examples/hello.deer

# Compile to native executable
./deer -o hello examples/hello.deer && ./hello

# Dump tokens
./deer -o /tmp/hello.tok examples/hello.deer

# Dump AST
./deer -o /tmp/hello.ast examples/hello.deer

# Dump LLVM IR
./deer -d examples/hello.deer

# Verbose debug output
./deer -v -e examples/hello.deer
```

Language Syntax
---------------
```
# Comments start with # and go to end of line

# External function declarations
extern printd(x)
extern putchard(x)

# Function definition
def fib(x)
  if x < 3 then
1
  else
    fib(x-1) + fib(x-2)

# Binary operator definition (precedence from 1-100)
def binary : 1 (x, y) y

# Unary operator definition
def unary !(v)
  if v then 0 else 1

# Mutable variables
def example()
  var a = 1, b = 2 in
  a + b

# For loops
def printstars(n)
  for i = 1, i < n, 1 in
    putchard(42)  # '*'
```

Examples
--------
See the [examples/](examples/) directory:
- [hello.deer](examples/hello.deer) - Print "Hello" using ASCII codes
- [math.deer](examples/math.deer) - Math function examples
- [operators.deer](examples/operators.deer) - User-defined operators
- [fib.deer](examples/fib.deer) - Iterative Fibonacci

Built-in functions
------------------
- `printd(x)` - Print a number as a float
- `printi(x)` - Print a number as integer
- `putchard(x)` - Print a single ASCII character
- `println(x)` - Print with formatted output
- Math functions via libc: `sin_`, `cos_`, `sqrt_`, `pow_`, `log_`, `exp_`, etc.

Architecture
------------
- `lib.c` / `lib.go` - C runtime functions and cgo bindings. `lib.c` defines
  all host functions (print, math) and is used both for JIT symbol registration
  and native linking. `lib.go` declares them as `extern` and provides Go
  wrappers for JIT execution.
- `codegen.go` - LLVM IR generation and optimization (PassBuilder API)
- `exec.go` - JIT execution engine (MCJIT)
- `parse.go` - Recursive descent parser
- `lex.go` - Lexer
- `nodes.go` - AST node types

Resources
---------
* [LLVM's Official C++ Kaleidoscope Tutorial](http://llvm.org/docs/tutorial/LangImpl1.html)
* [Rob Pike's *Lexical Scanning in Go*](http://www.youtube.com/watch?v=HxaD_trXwRE)
* [Go bindings to LLVM (tinygo)](https://github.com/tinygo-org/go-llvm)
* [TinyGo - Go for microcontrollers](https://github.com/tinygo-org/tinygo)

Changes from upstream
---------------------
* Replaced `go-spew` with [litter](https://github.com/sanity-io/litter) for cleaner Go-syntax dumps
* Added proper filename:line:column positions in errors and tokens
* Added support for `:` as a statement separator (in addition to `;`)
* Switched to `log/slog` for structured debug logging
* Added more C standard library math functions
* Migrated from deprecated PassManager API to PassBuilder API
* Fixed MCJIT multi-statement execution (two-phase: codegen all, then run)
* Registered host C functions via `LLVMAddSymbol` for JIT symbol resolution
* Added `-Wl,-export_dynamic` linker flag on macOS for JIT symbol visibility
