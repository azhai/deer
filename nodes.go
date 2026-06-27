package main

import (
	"fmt"
	"strings"

	"tinygo.org/x/go-llvm"
)

type node interface {
	Kind() nodeType
	Position() SrcPos
	codegen() llvm.Value
}

type nodeType int

type SrcPos struct {
	File string
	Line int
	Col  int
	Pos  int
}

func (p SrcPos) Position() SrcPos {
	return p
}

func (p SrcPos) String() string {
	if p.File != "" {
		return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

func (t nodeType) Kind() nodeType {
	return t
}

const (
	nodeNumber nodeType = iota
	nodeIf
	nodeFor
	nodeUnary
	nodeBinary
	nodeFnCall
	nodeVariable
	nodeVariableExpr
	nodeFnPrototype
	nodeFunction
	nodeList
	nodeBool
	nodeNil
	nodeSelf        // $ receiver reference
	nodeFieldAccess // expr.field
	nodeMethodCall  // expr.method(args)
	nodeStructDef   // struct definition
	nodeStructLit   // struct literal
	nodeBlock       // { expr1; expr2; ... }
)

var nodeTypeNames = map[nodeType]string{
	nodeNumber:       "Number",
	nodeIf:           "If",
	nodeFor:          "For",
	nodeUnary:        "Unary",
	nodeBinary:       "Binary",
	nodeFnCall:       "FnCall",
	nodeVariable:     "Variable",
	nodeVariableExpr: "VarExpr",
	nodeFnPrototype:  "FnProto",
	nodeFunction:     "Function",
	nodeList:         "List",
	nodeBool:         "Bool",
	nodeNil:          "Nil",
	nodeSelf:         "Self",
	nodeFieldAccess:  "FieldAccess",
	nodeMethodCall:   "MethodCall",
	nodeStructDef:    "StructDef",
	nodeStructLit:    "StructLit",
	nodeBlock:        "Block",
}

func (t nodeType) String() string {
	if name, ok := nodeTypeNames[t]; ok {
		return name
	}
	return fmt.Sprintf("NodeType(%d)", t)
}

type numberNode struct {
	nodeType
	SrcPos
	val   float64
	isInt bool
}

func (n *numberNode) String() string {
	if n.isInt {
		return fmt.Sprintf("%d", int64(n.val))
	}
	return fmt.Sprintf("%g", n.val)
}

type ifNode struct {
	nodeType
	SrcPos
	ifN   node
	thenN node
	elseN node
}

func (n *ifNode) String() string {
	var b strings.Builder
	b.WriteString("(if ")
	b.WriteString(fmt.Sprintf("%v", n.ifN))
	b.WriteString(" then ")
	b.WriteString(fmt.Sprintf("%v", n.thenN))
	if n.elseN != nil {
		b.WriteString(" else ")
		b.WriteString(fmt.Sprintf("%v", n.elseN))
	}
	b.WriteString(")")
	return b.String()
}

type forNode struct {
	nodeType
	SrcPos
	counter string
	start   node
	test    node
	step    node
	body    node
}

func (n *forNode) String() string {
	var b strings.Builder
	b.WriteString("(for ")
	b.WriteString(n.counter)
	b.WriteString(" = ")
	b.WriteString(fmt.Sprintf("%v", n.start))
	b.WriteString(", ")
	b.WriteString(fmt.Sprintf("%v", n.test))
	if n.step != nil {
		b.WriteString(", ")
		b.WriteString(fmt.Sprintf("%v", n.step))
	}
	b.WriteString(" in ")
	b.WriteString(fmt.Sprintf("%v", n.body))
	b.WriteString(")")
	return b.String()
}

type unaryNode struct {
	nodeType
	SrcPos
	name    string
	operand node
}

func (n *unaryNode) String() string {
	return fmt.Sprintf("(%s%v)", n.name, n.operand)
}

type binaryNode struct {
	nodeType
	SrcPos
	op    string
	left  node
	right node
}

func (n *binaryNode) String() string {
	return fmt.Sprintf("(%s %v %v)", n.op, n.left, n.right)
}

type fnCallNode struct {
	nodeType
	SrcPos
	callee string
	args   []node
}

func (n *fnCallNode) String() string {
	var b strings.Builder
	b.WriteString("(")
	b.WriteString(n.callee)
	for _, arg := range n.args {
		b.WriteString(" ")
		b.WriteString(fmt.Sprintf("%v", arg))
	}
	b.WriteString(")")
	return b.String()
}

type variableNode struct {
	nodeType
	SrcPos
	name string
}

func (n *variableNode) String() string {
	return n.name
}

type variableExprNode struct {
	nodeType
	SrcPos
	vars []struct {
		name string
		node node
	}
	body node
}

func (n *variableExprNode) String() string {
	var b strings.Builder
	b.WriteString("(var ")
	for i, v := range n.vars {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(v.name)
		if v.node != nil {
			b.WriteString(" = ")
			b.WriteString(fmt.Sprintf("%v", v.node))
		}
	}
	b.WriteString(" in ")
	b.WriteString(fmt.Sprintf("%v", n.body))
	b.WriteString(")")
	return b.String()
}

type fnPrototypeNode struct {
	nodeType
	SrcPos
	name         string
	args         []string
	isOperator   bool
	precedence   int
	paramTypes   []TypeKind // type for each parameter (TypeInt if unspecified)
	retType      TypeKind   // return type (TypeInt if unspecified)
	receiverType string     // struct type name for method receiver (empty for plain functions)
}

func (n *fnPrototypeNode) String() string {
	var b strings.Builder
	if n.isOperator {
		b.WriteString("operator ")
	}
	if n.receiverType != "" {
		b.WriteString(n.receiverType)
		b.WriteString(".")
	}
	b.WriteString(n.name)
	b.WriteString("(")
	for i, arg := range n.args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(arg)
		if i < len(n.paramTypes) && n.paramTypes[i] != TypeInt {
			b.WriteString(" ")
			b.WriteString(n.paramTypes[i].String())
		}
	}
	b.WriteString(")")
	if n.retType != TypeInt && n.retType != TypeVoid {
		b.WriteString(" ")
		b.WriteString(n.retType.String())
	}
	return b.String()
}

type functionNode struct {
	nodeType
	SrcPos
	proto node
	body  node
}

func (n *functionNode) String() string {
	var b strings.Builder
	p := n.proto.(*fnPrototypeNode)
	if p.name == "" {
		b.WriteString(fmt.Sprintf("%v", n.body))
	} else {
		b.WriteString("(def ")
		b.WriteString(fmt.Sprintf("%v", n.proto))
		b.WriteString(" ")
		b.WriteString(fmt.Sprintf("%v", n.body))
		b.WriteString(")")
	}
	return b.String()
}

type listNode struct {
	nodeType
	SrcPos
	nodes []node
}

func (n *listNode) String() string {
	var b strings.Builder
	b.WriteString("(begin")
	for _, node := range n.nodes {
		b.WriteString("\n  ")
		b.WriteString(fmt.Sprintf("%v", node))
	}
	b.WriteString(")")
	return b.String()
}

// --- New nodes for type system ---

type boolNode struct {
	nodeType
	SrcPos
	val bool
}

func (n *boolNode) String() string {
	if n.val {
		return "true"
	}
	return "false"
}

type nilNode struct {
	nodeType
	SrcPos
}

func (n *nilNode) String() string { return "nil" }

// selfNode represents the $ receiver reference inside a method.
type selfNode struct {
	nodeType
	SrcPos
	structName string // resolved struct type name (set during parsing)
}

func (n *selfNode) String() string { return "$" }

// fieldAccessNode represents expr.field access.
type fieldAccessNode struct {
	nodeType
	SrcPos
	object node
	field  string
}

func (n *fieldAccessNode) String() string {
	return fmt.Sprintf("%v.%s", n.object, n.field)
}

// methodCallNode represents expr.method(args...) call.
type methodCallNode struct {
	nodeType
	SrcPos
	object node
	method string
	args   []node
}

func (n *methodCallNode) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%v.%s(", n.object, n.method))
	for i, arg := range n.args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("%v", arg))
	}
	b.WriteString(")")
	return b.String()
}

// structDefNode represents a struct type definition.
type structDefNode struct {
	nodeType
	SrcPos
	name   string
	parent string
	fields []StructField
}

func (n *structDefNode) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("(struct %s", n.name))
	if n.parent != "" {
		b.WriteString(fmt.Sprintf(" : %s", n.parent))
	}
	for _, f := range n.fields {
		b.WriteString(fmt.Sprintf("\n  .%s %s", f.Name, f.Type))
	}
	b.WriteString(")")
	return b.String()
}

// structLitNode represents a struct literal expression.
type structLitNode struct {
	nodeType
	SrcPos
	typeName string
	fields   []struct {
		name string
		val  node
	}
}

func (n *structLitNode) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("(%s{", n.typeName))
	for i, f := range n.fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("%s: %v", f.name, f.val))
	}
	b.WriteString("})")
	return b.String()
}

// blockNode represents a block of expressions { expr1; expr2; ... }.
// Returns the value of the last expression.
type blockNode struct {
	nodeType
	SrcPos
	stmts []node
}

func (n *blockNode) String() string {
	var b strings.Builder
	b.WriteString("(block")
	for _, s := range n.stmts {
		b.WriteString("\n  ")
		b.WriteString(fmt.Sprintf("%v", s))
	}
	b.WriteString(")")
	return b.String()
}
