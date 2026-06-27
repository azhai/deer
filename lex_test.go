package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lexString runs the lexer on src and returns all non-trivial tokens
// (skipping tokSpace, tokComment, tokNewFile, tokEOF).
func lexString(t *testing.T, src string) []token {
	t.Helper()
	f := writeTempFile(t, src)
	defer os.Remove(f.Name())
	lex := Lex()
	go func() {
		lex.Add(f)
		lex.Done()
	}()
	var toks []token
	for tok := range lex.Tokens() {
		if tok.kind == tokSpace || tok.kind == tokComment ||
			tok.kind == tokNewFile || tok.kind == tokEOF {
			continue
		}
		toks = append(toks, tok)
	}
	return toks
}

// lexStringTokens runs the lexer and returns ALL tokens including errors.
func lexStringAll(t *testing.T, src string) []token {
	t.Helper()
	f := writeTempFile(t, src)
	defer os.Remove(f.Name())
	lex := Lex()
	go func() {
		lex.Add(f)
		lex.Done()
	}()
	var toks []token
	for tok := range lex.Tokens() {
		toks = append(toks, tok)
	}
	return toks
}

func writeTempFile(t *testing.T, src string) *os.File {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.dr")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(src); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("seek temp file: %v", err)
	}
	return f
}

func TestLexNumber(t *testing.T) {
	toks := lexString(t, "42 3.14")
	if len(toks) != 2 {
		t.Fatalf("expected 2 tokens, got %d: %v", len(toks), toks)
	}
	if toks[0].kind != tokNumber || toks[0].val != "42" {
		t.Errorf("toks[0] = %v, want Number(42)", toks[0])
	}
	if toks[1].kind != tokNumber || toks[1].val != "3.14" {
		t.Errorf("toks[1] = %v, want Number(3.14)", toks[1])
	}
}

func TestLexIdentifier(t *testing.T) {
	toks := lexString(t, "foo bar_123")
	if len(toks) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(toks))
	}
	for _, tok := range toks {
		if tok.kind != tokIdentifier {
			t.Errorf("token %v not Identifier", tok)
		}
	}
	if toks[0].val != "foo" || toks[1].val != "bar_123" {
		t.Errorf("got %q %q", toks[0].val, toks[1].val)
	}
}

func TestLexKeywords(t *testing.T) {
	src := "def extern if then else for in binary unary var struct nil true false"
	toks := lexString(t, src)
	expectedKinds := []tokenType{
		tokDefine, tokExtern, tokIf, tokThen, tokElse,
		tokFor, tokIn, tokBinary, tokUnary, tokVariable,
		tokStruct, tokNil, tokTrue, tokFalse,
	}
	if len(toks) != len(expectedKinds) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expectedKinds), len(toks), toks)
	}
	for i, want := range expectedKinds {
		if toks[i].kind != want {
			t.Errorf("toks[%d].kind = %v, want %v", i, toks[i].kind, want)
		}
	}
}

func TestLexTypeKeywords(t *testing.T) {
	src := "void bool byte short rune int float str"
	toks := lexString(t, src)
	// Type names are not keywords in the lexer; they are emitted as
	// tokIdentifier and resolved to types by the parser. Here we only verify
	// the values come through intact.
	expected := []string{"void", "bool", "byte", "short", "rune", "int", "float", "str"}
	if len(toks) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(toks), toks)
	}
	for i, want := range expected {
		if toks[i].kind != tokIdentifier {
			t.Errorf("toks[%d].kind = %v, want Identifier", i, toks[i].kind)
		}
		if toks[i].val != want {
			t.Errorf("toks[%d].val = %q, want %q", i, toks[i].val, want)
		}
	}
}

func TestLexOperators(t *testing.T) {
	toks := lexString(t, "+ - * / % < > = & |")
	expectedKinds := []tokenType{
		tokPlus, tokMinus, tokStar, tokSlash, tokPercent,
		tokLessThan, tokGreaterThan, tokEqual, tokAmp, tokPipe,
	}
	if len(toks) != len(expectedKinds) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expectedKinds), len(toks), toks)
	}
	for i, want := range expectedKinds {
		if toks[i].kind != want {
			t.Errorf("toks[%d].kind = %v, want %v", i, toks[i].kind, want)
		}
	}
}

func TestLexPunctuation(t *testing.T) {
	toks := lexString(t, "(){};,:.$")
	expectedKinds := []tokenType{
		tokLeftParen, tokRightParen, tokLBrace, tokRBrace,
		tokSemicolon, tokComma, tokColon, tokDot, tokDollar,
	}
	if len(toks) != len(expectedKinds) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expectedKinds), len(toks), toks)
	}
	for i, want := range expectedKinds {
		if toks[i].kind != want {
			t.Errorf("toks[%d].kind = %v, want %v", i, toks[i].kind, want)
		}
	}
}

func TestLexStringLiteral(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`'hello'`, "hello"},
		{`''`, ""},
		{`'hello world'`, "hello world"},
		{`'with \' quote'`, "with ' quote"},
		{`'with \\ backslash'`, "with \\ backslash"},
		{`'line\nbreak'`, "line\nbreak"},
		{`'tab\there'`, "tab\there"},
	}
	for _, c := range cases {
		toks := lexString(t, c.src)
		if len(toks) != 1 {
			t.Errorf("lex %q: expected 1 token, got %d: %v", c.src, len(toks), toks)
			continue
		}
		if toks[0].kind != tokStringLit {
			t.Errorf("lex %q: kind = %v, want StringLit", c.src, toks[0].kind)
			continue
		}
		if toks[0].val != c.want {
			t.Errorf("lex %q: val = %q, want %q", c.src, toks[0].val, c.want)
		}
	}
}

func TestLexStringLiteralUnterminated(t *testing.T) {
	toks := lexStringAll(t, "'unterminated")
	var gotError bool
	for _, tok := range toks {
		if tok.kind == tokError {
			gotError = true
			if !strings.Contains(tok.val, "unterminated string literal") {
				t.Errorf("error msg = %q, want 'unterminated string literal'", tok.val)
			}
		}
	}
	if !gotError {
		t.Error("expected an error for unterminated string literal")
	}
}

func TestLexQuotedOp(t *testing.T) {
	toks := lexString(t, "def `:` binary:1")
	// Expected: def, `:`, binary, :, 1
	if len(toks) < 4 {
		t.Fatalf("expected at least 4 tokens, got %d: %v", len(toks), toks)
	}
	// Find the QuotedOp token.
	var found bool
	for _, tok := range toks {
		if tok.kind == tokQuotedOp {
			found = true
			if tok.val != ":" {
				t.Errorf("QuotedOp val = %q, want ':'", tok.val)
			}
		}
	}
	if !found {
		t.Errorf("no QuotedOp token found in %v", toks)
	}
}

func TestLexComment(t *testing.T) {
	toks := lexString(t, "# this is a comment\n42")
	// Only the number token should remain (comment + newline-space skipped).
	if len(toks) != 1 {
		t.Fatalf("expected 1 token, got %d: %v", len(toks), toks)
	}
	if toks[0].kind != tokNumber || toks[0].val != "42" {
		t.Errorf("got %v, want Number(42)", toks[0])
	}
}

func TestLexUserOperatorRegistration(t *testing.T) {
	lex := Lex()
	lex.RegisterBinaryOp(':')
	f := writeTempFile(t, ":")
	defer os.Remove(f.Name())
	go func() {
		lex.Add(f)
		lex.Done()
	}()
	var toks []token
	for tok := range lex.Tokens() {
		if tok.kind == tokSpace || tok.kind == tokNewFile || tok.kind == tokEOF {
			continue
		}
		toks = append(toks, tok)
	}
	if len(toks) != 1 {
		t.Fatalf("expected 1 token, got %d", len(toks))
	}
	if toks[0].kind != tokUserBinaryOp {
		t.Errorf("got kind %v, want tokUserBinaryOp", toks[0].kind)
	}
	if toks[0].val != ":" {
		t.Errorf("got val %q, want ':'", toks[0].val)
	}
}
