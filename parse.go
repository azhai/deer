package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/sanity-io/litter"
	"tinygo.org/x/go-llvm"
)

type parser struct {
	name               string
	tokens             <-chan token
	token              token
	topLevelNodes      chan node
	binaryOpPrecedence map[string]int
}

func Parse(tokens <-chan token) <-chan node {
	p := &parser{
		tokens:        tokens,
		topLevelNodes: make(chan node, 100),
		binaryOpPrecedence: map[string]int{
			"=": 2,
			"|": 6,
			"&": 8,
			"<": 10,
			">": 10,
			"+": 20,
			"-": 20,
			"*": 40,
			"/": 40,
			"%": 40,
		},
	}
	go p.parse()
	return p.topLevelNodes
}

func (p *parser) parse() {
	for p.next(); p.token.kind > tokError; {
		topLevelNode := p.parseTopLevelStmt()
		if topLevelNode != nil {
			p.topLevelNodes <- topLevelNode
		}
	}

	if p.token.kind == tokError {
		fmt.Fprintf(os.Stderr, "Lex error at %s: %s\n", p.token.pos, p.token.val)
		litter.Dump(p.token)
	}
	close(p.topLevelNodes)
}

func (p *parser) next() token {
	for p.token = <-p.tokens; p.token.kind == tokSpace ||
		p.token.kind == tokComment; p.token = <-p.tokens {
	}
	return p.token
}

func (p *parser) parseTopLevelStmt() node {
	// Once `:` is defined as a user binary operator (via `def binary :`),
	// it is tokenized as tokUserBinaryOp instead of tokColon. Handle both.
	if p.token.kind == tokUserBinaryOp && p.token.val == ":" {
		p.next()
		return nil
	}
	switch p.token.kind {
	case tokNewFile:
		p.name = p.token.val
		p.next()
		return nil
	case tokSemicolon, tokColon:
		p.next()
		return nil
	case tokDefine:
		return p.parseDefinition()
	case tokExtern:
		return p.parseExtern()
	default:
		return p.parseTopLevelExpr()
	}
}

func (p *parser) parseDefinition() node {
	pos := p.token.pos
	p.next()
	proto := p.parsePrototype()
	if proto == nil {
		return nil
	}

	e := p.parseExpression()
	if e == nil {
		return nil
	}
	return &functionNode{nodeFunction, pos, proto, e}
}

func (p *parser) parseExtern() node {
	p.next()
	return p.parsePrototype()
}

var anonFuncCounter int

func (p *parser) parseTopLevelExpr() node {
	pos := p.token.pos
	e := p.parseExpression()
	if e == nil {
		return nil
	}
	anonFuncCounter++
	name := fmt.Sprintf("__anon%d", anonFuncCounter)
	proto := &fnPrototypeNode{nodeFnPrototype, pos, name, nil, false, 0}
	f := &functionNode{nodeFunction, pos, proto, e}
	return f
}

func (p *parser) parsePrototype() node {
	pos := p.token.pos
	if p.token.kind != tokIdentifier &&
		p.token.kind != tokBinary &&
		p.token.kind != tokUnary {
		return Error(p.token, "expected function name in prototype")
	}

	fnName := p.token.val
	p.next()

	precedence := 30
	const (
		idef = iota
		unary
		binary
	)
	kind := idef

	switch fnName {
	case "unary":
		fnName += p.token.val
		kind = unary
		p.next()
	case "binary":
		fnName += p.token.val
		op := p.token.val
		kind = binary
		p.next()

		if p.token.kind == tokNumber {
			var err error
			precedence, err = strconv.Atoi(p.token.val)
			if err != nil {
				return Error(p.token, "invalid precedence")
			}
			p.next()
		}
		p.binaryOpPrecedence[op] = precedence
	}

	if p.token.kind != tokLeftParen {
		return Error(p.token, "expected '(' in prototype")
	}

	argNames := []string{}
	for p.next(); p.token.kind == tokIdentifier || p.token.kind == tokComma; p.next() {
		if p.token.kind != tokComma {
			argNames = append(argNames, p.token.val)
		}
	}
	if p.token.kind != tokRightParen {
		return Error(p.token, "expected ')' in prototype")
	}

	p.next()
	if kind != idef && len(argNames) != kind {
		return Error(p.token, "invalid number of operands for operator")
	}
	return &fnPrototypeNode{nodeFnPrototype, pos, fnName, argNames, kind != idef, precedence}
}

func (p *parser) parseExpression() node {
	lhs := p.parseUnary()
	if lhs == nil {
		return nil
	}

	return p.parseBinaryOpRHS(1, lhs)
}

func (p *parser) parseUnary() node {
	pos := p.token.pos
	if p.token.kind < tokUserUnaryOp {
		return p.parsePrimary()
	}

	name := p.token.val
	p.next()
	operand := p.parseUnary()
	if operand != nil {
		return &unaryNode{nodeUnary, pos, name, operand}
	}
	return nil
}

func (p *parser) parseBinaryOpRHS(exprPrec int, lhs node) node {
	pos := p.token.pos
	for {
		if p.token.kind < tokUserUnaryOp {
			return lhs
		}
		tokenPrec := p.getTokenPrecedence(p.token.val)
		if tokenPrec < exprPrec {
			return lhs
		}
		binOp := p.token.val
		p.next()

		rhs := p.parseUnary()
		if rhs == nil {
			return nil
		}

		nextPrec := p.getTokenPrecedence(p.token.val)
		if tokenPrec < nextPrec {
			rhs = p.parseBinaryOpRHS(tokenPrec+1, rhs)
			if rhs == nil {
				return nil
			}
		}

		lhs = &binaryNode{nodeBinary, pos, binOp, lhs, rhs}
	}
}

func (p *parser) getTokenPrecedence(token string) int {
	return p.binaryOpPrecedence[token]
}

func (p *parser) parsePrimary() node {
	switch p.token.kind {
	case tokIdentifier:
		return p.parseIdentifierExpr()
	case tokIf:
		return p.parseIfExpr()
	case tokFor:
		return p.parseForExpr()
	case tokVariable:
		return p.parseVarExpr()
	case tokNumber:
		return p.parseNumericExpr()
	case tokLeftParen:
		return p.parseParenExpr()
	case tokEndOfTokens:
		return nil
	default:
		oldToken := p.token
		p.next()
		return Error(oldToken, "unknown token when expecting expression")
	}
}

func (p *parser) parseIdentifierExpr() node {
	pos := p.token.pos
	name := p.token.val
	p.next()
	if p.token.kind != tokLeftParen {
		return &variableNode{nodeVariable, pos, name}
	}
	args := []node{}
	for p.next(); p.token.kind != tokRightParen; {
		switch p.token.kind {
		case tokComma:
			p.next()
		default:
			arg := p.parseExpression()
			if arg == nil {
				return nil
			}
			args = append(args, arg)
		}
	}
	p.next()
	return &fnCallNode{nodeFnCall, pos, name, args}
}

func (p *parser) parseIfExpr() node {
	pos := p.token.pos
	p.next()
	ifE := p.parseExpression()
	if ifE == nil {
		return Error(p.token, "expected condition after 'if'")
	}

	if p.token.kind != tokThen {
		return Error(p.token, "expected 'then' after if condition")
	}
	p.next()
	thenE := p.parseExpression()
	if thenE == nil {
		return Error(p.token, "expected expression after 'then'")
	}

	if p.token.kind != tokElse {
		return Error(p.token, "expected 'else' after then expression")
	}
	p.next()
	elseE := p.parseExpression()
	if elseE == nil {
		return Error(p.token, "expected expression after 'else'")
	}

	return &ifNode{nodeIf, pos, ifE, thenE, elseE}
}

func (p *parser) parseForExpr() node {
	pos := p.token.pos
	p.next()
	if p.token.kind != tokIdentifier {
		return Error(p.token, "expected identifier after 'for'")
	}
	counter := p.token.val

	p.next()
	if p.token.kind != tokEqual {
		return Error(p.token, "expected '=' after 'for "+counter+"'")
	}

	p.next()
	start := p.parseExpression()
	if start == nil {
		return Error(p.token, "expected expression after 'for "+counter+" ='")
	}
	if p.token.kind != tokComma {
		return Error(p.token, "expected ',' after 'for' start expression")
	}

	p.next()
	end := p.parseExpression()
	if end == nil {
		return Error(p.token, "expected end expression after 'for' start expression")
	}

	var step node
	if p.token.kind == tokComma {
		p.next()
		if step = p.parseExpression(); step == nil {
			return Error(p.token, "invalid step expression after 'for'")
		}
	}

	if p.token.kind != tokIn {
		return Error(p.token, "expected 'in' after 'for' sub-expression")
	}

	p.next()
	body := p.parseExpression()
	if body == nil {
		return Error(p.token, "expected body expression after 'for ... in'")
	}

	return &forNode{nodeFor, pos, counter, start, end, step, body}
}

func (p *parser) parseVarExpr() node {
	pos := p.token.pos
	p.next()
	v := variableExprNode{
		nodeType: nodeVariableExpr,
		SrcPos:   pos,
		vars: []struct {
			name string
			node node
		}{},
		body: nil,
	}
	var val node

	if p.token.kind != tokIdentifier {
		return Error(p.token, "expected identifier after var")
	}
	for {
		name := p.token.val
		p.next()

		val = nil
		if p.token.kind == tokEqual {
			p.next()
			val = p.parseExpression()
			if val == nil {
				return Error(p.token, "initialization failed")
			}
		}
		v.vars = append(v.vars, struct {
			name string
			node node
		}{name, val})

		if p.token.kind != tokComma {
			break
		}
		p.next()

		if p.token.kind != tokIdentifier {
			return Error(p.token, "expected identifier after var")
		}
	}

	if p.token.kind != tokIn {
		return Error(p.token, "expected 'in' after 'var'")
	}
	p.next()

	v.body = p.parseExpression()
	if v.body == nil {
		return Error(p.token, "empty body in var expression")
	}
	return &v
}

func (p *parser) parseParenExpr() node {
	p.next()
	v := p.parseExpression()
	if v == nil {
		return nil
	}
	if p.token.kind != tokRightParen {
		return Error(p.token, "expected ')'")
	}
	p.next()
	return v
}

func (p *parser) parseNumericExpr() node {
	pos := p.token.pos
	val, err := strconv.ParseFloat(p.token.val, 64)
	p.next()
	if err != nil {
		return Error(p.token, "invalid number")
	}
	return &numberNode{nodeNumber, pos, val}
}

func Error(t token, str string) node {
	fmt.Fprintf(os.Stderr, "Error at %s: %s\n\tgot: %s (%q)\n", t.pos, str, t.kind, t.val)
	return nil
}

func ErrorV(str string) llvm.Value {
	fmt.Fprintf(os.Stderr, "Error: %s\n", str)
	return llvm.Value{nil}
}
