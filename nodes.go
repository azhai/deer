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
	val float64
}

func (n *numberNode) String() string {
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
	name       string
	args       []string
	isOperator bool
	precedence int
}

func (n *fnPrototypeNode) String() string {
	var b strings.Builder
	if n.isOperator {
		b.WriteString("operator ")
	}
	b.WriteString(n.name)
	b.WriteString("(")
	b.WriteString(strings.Join(n.args, ", "))
	b.WriteString(")")
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
