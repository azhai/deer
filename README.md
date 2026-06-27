Deer Lang
=========

Go port of [LLVM's Kaleidoscope Tutorial](http://llvm.org/docs/tutorial/LangImpl1.html) using the [tinygo](https://github.com/tinygo-org/tinygo) LLVM bindings.

Deer is a simple functional language demonstrating JIT compilation and native code generation via LLVM.

Features
--------
- LLVM-based JIT execution and native code generation
- User-defined unary/binary operators
- Mutable variables with `var`/`in` syntax
- Control flow: `if`/`then`/`else`, `for` loops
- String literals with escape sequences (`'hello\n'`)
- Struct types with fields and methods
- Statically typed parameters and return types (`int`, `float`, `str`, ...)
- S-expression style AST dumping for debugging
- Precise source locations (file:line:col) in error messages
- Structured logging via `log/slog`

Prerequisites
-------------
- LLVM 21 development libraries
- Clang 21 (for linking final executables)
- Go 1.23+

### Ubuntu/Debian
```bash
wget https://apt.llvm.org/llvm.sh
chmod +x llvm.sh
sudo ./llvm.sh 21
sudo apt install clang-21 liblld-21-dev
```

### macOS
```bash
brew install llvm@21
```

> **Note for macOS users:** If you have multiple LLVM versions installed (e.g.,
> `llvm` and `llvm@21`), your shell profile may export `CPLUS_INCLUDE_PATH`
> pointing at a different LLVM's libc++ headers. These headers may be
> incompatible with Clang 21. The Makefile automatically unsets
> `CPLUS_INCLUDE_PATH`, `C_INCLUDE_PATH`, and `LIBRARY_PATH` on macOS
> so that `llvm@21`'s bundled libc++ is used.

Building
--------
```bash
make build
```

The Makefile handles OS-specific configuration:
- **macOS**: Uses Homebrew's `llvm@21` clang/clang++, unsets conflicting
  environment variables, and adds `-Wl,-export_dynamic` so the JIT can
  resolve host C functions at runtime.
- **Linux**: Uses system `clang`/`clang++`.

Usage
-----
```bash
# JIT execute a program
./deer -e examples/hello.dr

# Compile to native executable
./deer -o hello examples/hello.dr && ./hello

# Dump tokens
./deer -o /tmp/hello.tok examples/hello.dr

# Dump AST
./deer -o /tmp/hello.ast examples/hello.dr

# Dump LLVM IR
./deer -d examples/hello.dr

# Verbose debug output
./deer -v -e examples/hello.dr
```

Language Syntax
---------------
```
# Comments start with # and go to end of line

# External function declarations
extern printd(x)
extern putchard(x)
extern print_str(s str)   # str-typed parameter

# Function definition
def fib(x)
  if x < 3 then
1
  else
    fib(x-1) + fib(x-2)

# Typed function definition (int is the default; float/str/etc. supported)
def add(x int, y int) int x + y

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

# String literals (single-quoted, with escapes)
def main()
  print_str('Hello, Deer!') :
  print_str('Escapes: \t tab, \n newline, \\ backslash, \' quote')

# Struct definition
def Point struct {
  .x int,
  .y int,
}

# Struct literal and field access
def origin() Point
  Point { x: 0, y: 0 }
```

### String Literals

String literals are written with single quotes and support escape sequences:

| Escape | Meaning        |
|--------|----------------|
| `\n`   | Newline        |
| `\t`   | Tab            |
| `\r`   | Carriage return|
| `\\`   | Backslash      |
| `\'`   | Single quote   |
| `\0`   | Null byte      |

Strings are represented at the LLVM IR level as `i8*` pointers to
null-terminated global byte arrays. The `str` type keyword marks a parameter
or return type as a string pointer.

### Type System

The following built-in types are recognized in parameter and return position:

| Type    | Kind     | LLVM IR        |
|---------|----------|----------------|
| `int`   | integer  | `i64`          |
| `float` | float    | `double`       |
| `str`   | string   | `i8*`          |
| `bool`  | integer  | `i64`          |
| `byte`  | integer  | `i64`          |
| `short` | integer  | `i64`          |
| `rune`  | integer  | `i64`          |
| `void`  | void     | `void`         |

Untyped parameters default to `int` in `def` declarations and `float` in
`extern` declarations (matching the original Kaleidoscope behaviour).

Examples
--------
See the [examples/](examples/) directory:
- [hello.dr](examples/hello.dr) - Print "Hello" using ASCII codes
- [math.dr](examples/math.dr) - Math function examples
- [operators.dr](examples/operators.dr) - User-defined operators
- [fib.dr](examples/fib.dr) - Iterative Fibonacci
- [strings.dr](examples/strings.dr) - String literals and `print_str`

Built-in functions
------------------
- `printd(x)` - Print a number as a float
- `printi(x)` - Print a number as integer
- `putchard(x)` - Print a single ASCII character
- `println(x)` - Print with formatted output
- `print_str(s)` - Print a null-terminated string followed by a newline
- Math functions via libc: `sin_`, `cos_`, `sqrt_`, `pow_`, `log_`, `exp_`, etc.

Testing
-------
The project ships with table-driven Go tests for the lexer, parser, and type
system. Run them with:

```bash
make test
# or directly:
go test ./...
```

Architecture
------------
- `lib.go` - C runtime functions and cgo bindings. Inline C defines
  all host functions (print, math, `print_str`) and is used both for JIT
  symbol registration and native linking. The Go file declares them as
  `extern` and provides Go wrappers for JIT execution.
- `codegen.go` - LLVM IR generation and optimization (PassBuilder API)
- `exec.go` - JIT execution engine (MCJIT)
- `parse.go` - Recursive descent parser
- `lex.go` - Lexer
- `nodes.go` - AST node types
- `types.go` - Type system: `TypeKind` enum, built-in type lookup, struct
  registry (`StructDef` / `StructField`)

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
