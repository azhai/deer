package main

import (
	"os"
	"strings"
	"testing"
)

// parseString lexes and parses src, returning all top-level nodes.
func parseString(t *testing.T, src string) []node {
	t.Helper()
	f := writeTempFile(t, src)
	defer os.Remove(f.Name())

	lex := Lex()
	go func() {
		lex.Add(f)
		lex.Done()
	}()
	var nodes []node
	for n := range Parse(lex.Tokens(), lex) {
		nodes = append(nodes, n)
	}
	return nodes
}

// unwrapAnon returns the body expression of an anonymous function node, or the
// node itself if it is not a function wrapper. Top-level bare expressions are
// wrapped in anonymous function nodes by parseTopLevelExpr.
func unwrapAnon(n node) node {
	if fn, ok := n.(*functionNode); ok {
		if proto := fn.proto.(*fnPrototypeNode); strings.HasPrefix(proto.name, "__anon") {
			return fn.body
		}
	}
	return n
}

func TestParseNumber(t *testing.T) {
	nodes := parseString(t, "42")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	num, ok := unwrapAnon(nodes[0]).(*numberNode)
	if !ok {
		t.Fatalf("node type = %T, want *numberNode", unwrapAnon(nodes[0]))
	}
	if num.val != 42 {
		t.Errorf("val = %v, want 42", num.val)
	}
	if !num.isInt {
		t.Errorf("isInt = false, want true")
	}
}

func TestParseFloatLiteral(t *testing.T) {
	nodes := parseString(t, "3.14")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	num, ok := unwrapAnon(nodes[0]).(*numberNode)
	if !ok {
		t.Fatalf("node type = %T, want *numberNode", unwrapAnon(nodes[0]))
	}
	if num.val != 3.14 {
		t.Errorf("val = %v, want 3.14", num.val)
	}
	if num.isInt {
		t.Errorf("isInt = true, want false")
	}
}

func TestParseStringLiteral(t *testing.T) {
	nodes := parseString(t, "'hello'")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	s, ok := unwrapAnon(nodes[0]).(*stringLitNode)
	if !ok {
		t.Fatalf("node type = %T, want *stringLitNode", unwrapAnon(nodes[0]))
	}
	if s.val != "hello" {
		t.Errorf("val = %q, want 'hello'", s.val)
	}
}

func TestParseStringWithEscapes(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`'a\nb'`, "a\nb"},
		{`'a\tb'`, "a\tb"},
		{`'a\'b'`, "a'b"},
		{`'a\\b'`, "a\\b"},
	}
	for _, c := range cases {
		nodes := parseString(t, c.src)
		if len(nodes) != 1 {
			t.Errorf("parse %q: expected 1 node, got %d", c.src, len(nodes))
			continue
		}
		s, ok := unwrapAnon(nodes[0]).(*stringLitNode)
		if !ok {
			t.Errorf("parse %q: node type = %T", c.src, unwrapAnon(nodes[0]))
			continue
		}
		if s.val != c.want {
			t.Errorf("parse %q: val = %q, want %q", c.src, s.val, c.want)
		}
	}
}

func TestParseBoolLiteral(t *testing.T) {
	for src, want := range map[string]bool{"true": true, "false": false} {
		nodes := parseString(t, src)
		if len(nodes) != 1 {
			t.Fatalf("%s: expected 1 node, got %d", src, len(nodes))
		}
		b, ok := unwrapAnon(nodes[0]).(*boolNode)
		if !ok {
			t.Fatalf("%s: node type = %T, want *boolNode", src, unwrapAnon(nodes[0]))
		}
		if b.val != want {
			t.Errorf("%s: val = %v, want %v", src, b.val, want)
		}
	}
}

func TestParseNilLiteral(t *testing.T) {
	nodes := parseString(t, "nil")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if _, ok := unwrapAnon(nodes[0]).(*nilNode); !ok {
		t.Fatalf("node type = %T, want *nilNode", unwrapAnon(nodes[0]))
	}
}

func TestParseExtern(t *testing.T) {
	nodes := parseString(t, "extern printd(x)")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	// extern declarations emit a fnPrototypeNode directly.
	proto, ok := nodes[0].(*fnPrototypeNode)
	if !ok {
		t.Fatalf("node type = %T, want *fnPrototypeNode", nodes[0])
	}
	if proto.name != "printd" {
		t.Errorf("name = %q, want printd", proto.name)
	}
	if len(proto.args) != 1 {
		t.Fatalf("args len = %d, want 1", len(proto.args))
	}
	if proto.args[0] != "x" {
		t.Errorf("arg[0] = %q, want x", proto.args[0])
	}
	// Untyped extern param should be coerced to TypeFloat.
	if proto.paramTypes[0] != TypeFloat {
		t.Errorf("paramType[0] = %v, want TypeFloat", proto.paramTypes[0])
	}
}

func TestParseExternStr(t *testing.T) {
	nodes := parseString(t, "extern print_str(s str)")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	proto, ok := nodes[0].(*fnPrototypeNode)
	if !ok {
		t.Fatalf("node type = %T, want *fnPrototypeNode", nodes[0])
	}
	if proto.paramTypes[0] != TypeStr {
		t.Errorf("paramType[0] = %v, want TypeStr", proto.paramTypes[0])
	}
	if proto.retType != TypeStr {
		t.Errorf("retType = %v, want TypeStr", proto.retType)
	}
}

func TestParseDefinition(t *testing.T) {
	src := `def add(x, y) x + y`
	nodes := parseString(t, src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	fn, ok := nodes[0].(*functionNode)
	if !ok {
		t.Fatalf("node type = %T, want *functionNode", nodes[0])
	}
	proto := fn.proto.(*fnPrototypeNode)
	if proto.name != "add" {
		t.Errorf("name = %q, want add", proto.name)
	}
	if len(proto.args) != 2 {
		t.Fatalf("args len = %d, want 2", len(proto.args))
	}
	// Untyped def params default to TypeInt.
	for i, pt := range proto.paramTypes {
		if pt != TypeInt {
			t.Errorf("paramType[%d] = %v, want TypeInt", i, pt)
		}
	}
}

func TestParseDefinitionTyped(t *testing.T) {
	src := `def add(x int, y int) int x + y`
	nodes := parseString(t, src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	fn, ok := nodes[0].(*functionNode)
	if !ok {
		t.Fatalf("node type = %T", nodes[0])
	}
	proto := fn.proto.(*fnPrototypeNode)
	if proto.retType != TypeInt {
		t.Errorf("retType = %v, want TypeInt", proto.retType)
	}
	for i, pt := range proto.paramTypes {
		if pt != TypeInt {
			t.Errorf("paramType[%d] = %v, want TypeInt", i, pt)
		}
	}
}

func TestParseBinaryOpDefinition(t *testing.T) {
	src := "def `:` binary:1 (x, y) y"
	nodes := parseString(t, src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	fn, ok := nodes[0].(*functionNode)
	if !ok {
		t.Fatalf("node type = %T", nodes[0])
	}
	proto := fn.proto.(*fnPrototypeNode)
	if proto.name != "binary:" {
		t.Errorf("name = %q, want 'binary:'", proto.name)
	}
	if !proto.isOperator {
		t.Error("isOperator = false, want true")
	}
	if proto.precedence != 1 {
		t.Errorf("precedence = %d, want 1", proto.precedence)
	}
}

func TestParseIfExpr(t *testing.T) {
	src := `if 1 < 2 then 10 else 20`
	nodes := parseString(t, src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if _, ok := unwrapAnon(nodes[0]).(*ifNode); !ok {
		t.Fatalf("node type = %T, want *ifNode", unwrapAnon(nodes[0]))
	}
}

func TestParseForExpr(t *testing.T) {
	src := `for i = 1, i < 10, 1 in i`
	nodes := parseString(t, src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if _, ok := unwrapAnon(nodes[0]).(*forNode); !ok {
		t.Fatalf("node type = %T, want *forNode", unwrapAnon(nodes[0]))
	}
}

func TestParseVarExpr(t *testing.T) {
	src := `var a = 1, b = 2 in a + b`
	nodes := parseString(t, src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if _, ok := unwrapAnon(nodes[0]).(*variableExprNode); !ok {
		t.Fatalf("node type = %T, want *variableExprNode", unwrapAnon(nodes[0]))
	}
}

func TestParseParenExpr(t *testing.T) {
	src := `(1 + 2) * 3`
	nodes := parseString(t, src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	// Outer expression should be a binary op (mul).
	bin, ok := unwrapAnon(nodes[0]).(*binaryNode)
	if !ok {
		t.Fatalf("node type = %T, want *binaryNode", unwrapAnon(nodes[0]))
	}
	if bin.op != "*" {
		t.Errorf("op = %q, want '*'", bin.op)
	}
}

func TestParseCallExpr(t *testing.T) {
	src := `add(1, 2)`
	nodes := parseString(t, src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	call, ok := unwrapAnon(nodes[0]).(*fnCallNode)
	if !ok {
		t.Fatalf("node type = %T, want *fnCallNode", unwrapAnon(nodes[0]))
	}
	if call.callee != "add" {
		t.Errorf("callee = %q, want add", call.callee)
	}
	if len(call.args) != 2 {
		t.Errorf("args len = %d, want 2", len(call.args))
	}
}

func TestParseStructDef(t *testing.T) {
	src := `def Point struct {
    .x int,
    .y int,
}`
	nodes := parseString(t, src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	sd, ok := nodes[0].(*structDefNode)
	if !ok {
		t.Fatalf("node type = %T, want *structDefNode", nodes[0])
	}
	if sd.name != "Point" {
		t.Errorf("name = %q, want Point", sd.name)
	}
	if len(sd.fields) != 2 {
		t.Fatalf("fields len = %d, want 2", len(sd.fields))
	}
	if sd.fields[0].Name != "x" || sd.fields[1].Name != "y" {
		t.Errorf("fields = %+v, want x,y", sd.fields)
	}
	// Verify struct was registered.
	def := lookupStruct("Point")
	if def == nil {
		t.Fatal("Point not registered in struct registry")
	}
}

func TestParseMultipleStmts(t *testing.T) {
	src := `extern printd(x)
def f(x) x + 1
f(1)`
	nodes := parseString(t, src)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
}
