package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sanity-io/litter"
	"tinygo.org/x/go-llvm"
)

type parser struct {
	name               string
	tokens             <-chan token
	token              token
	topLevelNodes      chan node
	binaryOpPrecedence map[string]int
	userUnaryOps       map[string]bool // set of defined unary operator names
	userBinaryOps      map[string]bool // set of defined binary operator names
	currentMethodType  string          // current struct type name when parsing method body
	lex                *lexer          // optional: used to register operators for the new `def \`op\`` syntax
}

func Parse(tokens <-chan token, lex *lexer) <-chan node {
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
		userUnaryOps:  map[string]bool{},
		userBinaryOps: map[string]bool{},
		lex:           lex,
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
	// Once `:` is defined as a user binary operator it is tokenized as
	// tokUserBinaryOp; in that case treat it as an expression, not a separator.
	if p.token.kind == tokUserBinaryOp && p.token.val == ":" {
		return p.parseTopLevelExpr()
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

// isTypeName returns true if the identifier is a recognized type name
// (builtin type keyword or known struct type).
func (p *parser) isTypeName(name string) bool {
	if _, ok := builtinType(name); ok {
		return true
	}
	if lookupStruct(name) != nil {
		return true
	}
	return false
}

// parseTypeRef parses a type name identifier and returns its TypeKind.
func (p *parser) parseTypeRef() TypeKind {
	if p.token.kind == tokIdentifier {
		if k, ok := builtinType(p.token.val); ok {
			p.next()
			return k
		}
		if lookupStruct(p.token.val) != nil {
			p.next()
			return TypeStruct
		}
	}
	return TypeStr // default type is now str
}

func (p *parser) parseDefinition() node {
	pos := p.token.pos
	p.next() // consume 'def'

	if p.token.kind != tokIdentifier &&
		p.token.kind != tokQuotedOp &&
		p.token.kind != tokBinary &&
		p.token.kind != tokUnary {
		return Error(p.token, "expected name after 'def'")
	}

	// New syntax: def `op` binary:prec (args...) or def `op` unary (arg)
	if p.token.kind == tokQuotedOp {
		opName := p.token.val // e.g. "+", "!"
		p.next()
		return p.parseQuotedOpDef(pos, opName)
	}

	name := p.token.val
	p.next()

	// Check what follows the name.
	switch p.token.kind {
	case tokStruct:
		// def Name struct { ... }
		return p.parseStructDef(pos, name, "")
	case tokColon:
		// def Name: Parent struct { ... }
		p.next()
		if p.token.kind != tokIdentifier {
			return Error(p.token, "expected parent type name after ':'")
		}
		parent := p.token.val
		p.next()
		if p.token.kind != tokStruct {
			return Error(p.token, "expected 'struct' after parent type name")
		}
		return p.parseStructDef(pos, name, parent)
	case tokDot:
		// def Name.Method(...) ... — method definition
		p.next()
		if p.token.kind != tokIdentifier {
			return Error(p.token, "expected method name after '.'")
		}
		methodName := p.token.val
		p.next() // advance past method name
		return p.parseMethodDef(pos, name, methodName)
	default:
		// Check for operator definitions: def binary/unary ...
		if name == "binary" || name == "unary" {
			// Reconstruct as if parsePrototype saw the operator keyword.
			proto := p.parseOperatorPrototype(pos, name)
			if proto == nil {
				return nil
			}
			var body node
			if p.token.kind == tokLBrace {
				body = p.parseBlock()
			} else {
				body = p.parseExpression()
			}
			if body == nil {
				return nil
			}
			return &functionNode{nodeFunction, pos, proto, body}
		}
		// Regular function definition.
		proto := p.parsePrototypeWithName(pos, name)
		if proto == nil {
			return nil
		}
		var body node
		if p.token.kind == tokLBrace {
			body = p.parseBlock()
		} else {
			body = p.parseExpression()
		}
		if body == nil {
			return nil
		}
		return &functionNode{nodeFunction, pos, proto, body}
	}
}

// parseQuotedOpDef parses the new operator definition syntax:
//
//	def `op` binary:PREC (args...) body
//	def `op` unary (arg) body
func (p *parser) parseQuotedOpDef(pos SrcPos, opName string) node {
	// Expect 'binary' or 'unary' keyword.
	if p.token.kind != tokBinary && p.token.kind != tokUnary {
		return Error(p.token, "expected 'binary' or 'unary' after quoted operator")
	}
	isBinary := p.token.kind == tokBinary
	p.next()

	prec := 30
	if isBinary {
		// Expect ':' followed by a number for precedence. The ':' may be
		// tokenized as tokColon or, if ':' itself was previously defined as a
		// binary operator, as tokUserBinaryOp.
		if p.token.val != ":" || (p.token.kind != tokColon && p.token.kind != tokUserBinaryOp) {
			return Error(p.token, "expected ':' after 'binary'")
		}
		p.next()
		if p.token.kind != tokNumber {
			return Error(p.token, "expected precedence number after 'binary:'")
		}
		num, err := strconv.Atoi(p.token.val)
		if err != nil {
			return Error(p.token, "invalid precedence")
		}
		prec = num
		p.next()
		// Register this operator precedence.
		p.binaryOpPrecedence[opName] = prec
	}

	// Build fnName: "binary+" or "unary!".
	var fnName string
	if isBinary {
		fnName = "binary" + opName
		p.userBinaryOps[opName] = true
		// Register the operator char with the lexer so that subsequent uses
		// of the operator are tokenized as tokUserBinaryOp.
		if p.lex != nil && len(opName) == 1 {
			p.lex.RegisterBinaryOp([]rune(opName)[0])
		}
	} else {
		fnName = "unary" + opName
		p.userUnaryOps[opName] = true
		if p.lex != nil && len(opName) == 1 {
			p.lex.RegisterUnaryOp([]rune(opName)[0])
		}
	}

	// Parse parameter list.
	if p.token.kind != tokLeftParen {
		return Error(p.token, "expected '(' in operator definition")
	}
	argNames, argTypes, err := p.parseParamList()
	if err != nil {
		return Error(p.token, err.Error())
	}

	// Validate argument count.
	expectedArgs := 1
	if isBinary {
		expectedArgs = 2
	}
	if len(argNames) != expectedArgs {
		return Error(p.token, fmt.Sprintf("operator expects %d argument(s), got %d", expectedArgs, len(argNames)))
	}

	proto := &fnPrototypeNode{
		nodeType:   nodeFnPrototype,
		SrcPos:     pos,
		name:       fnName,
		args:       argNames,
		paramTypes: argTypes,
		isOperator: true,
		precedence: prec,
		retType:    TypeStr, // default return type is now str
	}

	// Parse body.
	var body node
	if p.token.kind == tokLBrace {
		body = p.parseBlock()
	} else {
		body = p.parseExpression()
	}
	if body == nil {
		return nil
	}
	return &functionNode{nodeFunction, pos, proto, body}
}

func (p *parser) parseExtern() node {
	p.next()
	return p.parsePrototype()
}

func (p *parser) parseStructDef(pos SrcPos, name, parent string) node {
	// Current token is 'struct'.
	p.next()
	if p.token.kind != tokLBrace {
		return Error(p.token, "expected '{' in struct definition")
	}
	p.next()

	var fields []StructField
	for p.token.kind != tokRBrace {
		if p.token.kind == tokComma || p.token.kind == tokSemicolon {
			p.next()
			continue
		}
		if p.token.kind != tokDot {
			return Error(p.token, "expected '.' before field name")
		}
		p.next()
		if p.token.kind != tokIdentifier {
			return Error(p.token, "expected field name after '.'")
		}
		fieldName := p.token.val
		private := strings.HasPrefix(fieldName, "_")
		p.next()
		fieldType := p.parseTypeRef()
		fields = append(fields, StructField{
			Name:    fieldName,
			Type:    fieldType,
			Private: private,
		})
	}
	p.next() // consume '}'

	def := &StructDef{
		Name:    name,
		Parent:  parent,
		Fields:  fields,
		Methods: map[string]*fnPrototypeNode{},
	}
	registerStruct(def)
	return &structDefNode{
		nodeType: nodeStructDef,
		SrcPos:   pos,
		name:     name,
		parent:   parent,
		fields:   fields,
	}
}

func (p *parser) parseMethodDef(pos SrcPos, typeName, methodName string) node {
	proto := &fnPrototypeNode{
		nodeType:     nodeFnPrototype,
		SrcPos:       pos,
		name:         methodName,
		receiverType: typeName,
		args:         []string{"$"},
		paramTypes:   []TypeKind{TypeStruct},
		retType:      TypeVoid,
	}

	if p.token.kind != tokLeftParen {
		return Error(p.token, "expected '(' in method definition")
	}

	// Parse explicit parameters (after the implicit $ receiver).
	extraArgs, extraTypes, err := p.parseParamList()
	if err != nil {
		return Error(p.token, err.Error())
	}
	proto.args = append(proto.args, extraArgs...)
	proto.paramTypes = append(proto.paramTypes, extraTypes...)

	// Parse optional return type.
	if p.token.kind == tokIdentifier && p.isTypeName(p.token.val) {
		proto.retType = p.parseTypeRef()
	}

	// Register method in struct.
	if sd := lookupStruct(typeName); sd != nil {
		sd.Methods[methodName] = proto
	}

	// Set current method type so $ references know their struct type.
	oldMethod := p.currentMethodType
	p.currentMethodType = typeName
	defer func() { p.currentMethodType = oldMethod }()

	// Parse body.
	var body node
	if p.token.kind == tokLBrace {
		body = p.parseBlock()
	} else {
		body = p.parseExpression()
	}
	if body == nil {
		return nil
	}
	return &functionNode{nodeFunction, pos, proto, body}
}

// parseOperatorPrototype parses the rest of an operator prototype after
// the keyword ('binary' or 'unary') has been consumed as fnName.
func (p *parser) parseOperatorPrototype(pos SrcPos, fnName string) node {
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
		// Register binary operator.
		p.userBinaryOps[op] = true
	}
	if kind == unary {
		// fnName is "unaryX", operator name is fnName[5:]
		opName := fnName[5:]
		p.userUnaryOps[opName] = true
	}

	if p.token.kind != tokLeftParen {
		return Error(p.token, "expected '(' in prototype")
	}

	argNames, argTypes, err := p.parseParamList()
	if err != nil {
		return Error(p.token, err.Error())
	}

	if kind != idef && len(argNames) != kind {
		return Error(p.token, "invalid number of operands for operator")
	}
	return &fnPrototypeNode{
		nodeType:   nodeFnPrototype,
		SrcPos:     pos,
		name:       fnName,
		args:       argNames,
		paramTypes: argTypes,
		isOperator: kind != idef,
		precedence: precedence,
		retType:    TypeStr, // default return type is now str
	}
}

// parsePrototypeWithName parses the rest of a function prototype given the
// function name has already been consumed.
func (p *parser) parsePrototypeWithName(pos SrcPos, fnName string) node {
	argNames, argTypes, err := p.parseParamList()
	if err != nil {
		return Error(p.token, err.Error())
	}

	retType := TypeStr // default return type is now str
	if p.token.kind == tokIdentifier && p.isTypeName(p.token.val) {
		retType = p.parseTypeRef()
	}

	return &fnPrototypeNode{
		nodeType:   nodeFnPrototype,
		SrcPos:     pos,
		name:       fnName,
		args:       argNames,
		paramTypes: argTypes,
		retType:    retType,
	}
}

func (p *parser) parseBlock() node {
	pos := p.token.pos
	if p.token.kind != tokLBrace {
		return Error(p.token, "expected '{'")
	}
	p.next()

	var stmts []node
	for p.token.kind != tokRBrace && p.token.kind != tokEndOfTokens {
		if p.token.kind == tokSemicolon {
			p.next()
			continue
		}
		s := p.parseExpression()
		if s == nil {
			return nil
		}
		stmts = append(stmts, s)
	}
	if p.token.kind != tokRBrace {
		return Error(p.token, "expected '}'")
	}
	p.next()

	if len(stmts) == 1 {
		return stmts[0]
	}
	return &blockNode{nodeType: nodeBlock, SrcPos: pos, stmts: stmts}
}

var anonFuncCounter int

// parseParamList parses a comma-separated parameter list.
// Current token must be tokLeftParen. On return, token is the one after ')'.
func (p *parser) parseParamList() ([]string, []TypeKind, error) {
	if p.token.kind != tokLeftParen {
		return nil, nil, fmt.Errorf("expected '('")
	}
	p.next()

	argNames := []string{}
	argTypes := []TypeKind{}
	for p.token.kind == tokIdentifier {
		argName := p.token.val
		p.next()
		argType := TypeStr // default type is now str
		if p.token.kind == tokIdentifier && p.isTypeName(p.token.val) {
			argType = p.parseTypeRef()
		}
		argNames = append(argNames, argName)
		argTypes = append(argTypes, argType)
		if p.token.kind == tokComma {
			p.next()
		}
	}
	if p.token.kind != tokRightParen {
		return nil, nil, fmt.Errorf("expected ')' in prototype")
	}
	p.next()
	return argNames, argTypes, nil
}

func (p *parser) parseTopLevelExpr() node {
	pos := p.token.pos
	e := p.parseExpression()
	if e == nil {
		return nil
	}
	anonFuncCounter++
	name := fmt.Sprintf("__anon%d", anonFuncCounter)
	proto := &fnPrototypeNode{
		nodeType: nodeFnPrototype,
		SrcPos:   pos,
		name:     name,
		retType:  TypeStr, // default return type is now str
	}
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
		// Register binary operator.
		p.userBinaryOps[op] = true
	}
	if kind == unary {
		// fnName is "unaryX", operator name is fnName[5:]
		opName := fnName[5:]
		p.userUnaryOps[opName] = true
	}

	if p.token.kind != tokLeftParen {
		return Error(p.token, "expected '(' in prototype")
	}

	argNames, argTypes, err := p.parseParamList()
	if err != nil {
		return Error(p.token, err.Error())
	}

	if kind != idef && len(argNames) != kind {
		return Error(p.token, "invalid number of operands for operator")
	}
	// Extern functions wrap C functions that use double for all params and return.
	for i := range argTypes {
		if argTypes[i] == TypeInt || argTypes[i] == TypeStr {
			argTypes[i] = TypeFloat
		}
	}
	return &fnPrototypeNode{
		nodeType:   nodeFnPrototype,
		SrcPos:     pos,
		name:       fnName,
		args:       argNames,
		paramTypes: argTypes,
		isOperator: kind != idef,
		precedence: precedence,
		retType:    TypeFloat,
	}
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

	// Check for quoted unary operator: `op`
	if p.token.kind == tokQuotedOp && p.userUnaryOps[p.token.val] {
		name := p.token.val
		p.next()
		operand := p.parseUnary()
		if operand != nil {
			return &unaryNode{nodeUnary, pos, name, operand}
		}
		return nil
	}

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
		// Check if current token can be a binary operator.
		if p.token.kind < tokUserUnaryOp && p.token.kind != tokQuotedOp {
			return lhs
		}
		// For tokQuotedOp, only accept if it's a registered binary op.
		if p.token.kind == tokQuotedOp && !p.userBinaryOps[p.token.val] {
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
	case tokDollar:
		return p.parseSelfExpr()
	case tokTrue:
		return p.parseBoolExpr(true)
	case tokFalse:
		return p.parseBoolExpr(false)
	case tokNil:
		return p.parseNilExpr()
	case tokLBrace:
		return p.parseBlock()
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

	var expr node
	if p.token.kind != tokLeftParen {
		expr = &variableNode{nodeVariable, pos, name}
	} else {
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
		expr = &fnCallNode{nodeFnCall, pos, name, args}
	}

	// Handle postfix .field and .method(args).
	for p.token.kind == tokDot {
		p.next()
		if p.token.kind != tokIdentifier {
			return Error(p.token, "expected field/method name after '.'")
		}
		memberName := p.token.val
		p.next()
		if p.token.kind == tokLeftParen {
			// Method call: expr.method(args)
			args := []node{}
			for p.next(); p.token.kind != tokRightParen; {
				if p.token.kind == tokComma {
					p.next()
					continue
				}
				arg := p.parseExpression()
				if arg == nil {
					return nil
				}
				args = append(args, arg)
			}
			p.next()
			expr = &methodCallNode{
				nodeType: nodeMethodCall,
				SrcPos:   pos,
				object:   expr,
				method:   memberName,
				args:     args,
			}
		} else {
			// Field access: expr.field
			expr = &fieldAccessNode{
				nodeType: nodeFieldAccess,
				SrcPos:   pos,
				object:   expr,
				field:    memberName,
			}
		}
	}

	return expr
}

func (p *parser) parseSelfExpr() node {
	pos := p.token.pos
	p.next()
	var expr node = &selfNode{nodeType: nodeSelf, SrcPos: pos, structName: p.currentMethodType}

	// Handle $.field and $.method(args) postfix.
	for p.token.kind == tokDot {
		p.next()
		if p.token.kind != tokIdentifier {
			return Error(p.token, "expected field/method name after '$.'")
		}
		memberName := p.token.val
		p.next()
		if p.token.kind == tokLeftParen {
			// Method call: $.method(args)
			args := []node{}
			for p.next(); p.token.kind != tokRightParen; {
				if p.token.kind == tokComma {
					p.next()
					continue
				}
				arg := p.parseExpression()
				if arg == nil {
					return nil
				}
				args = append(args, arg)
			}
			p.next()
			expr = &methodCallNode{
				nodeType: nodeMethodCall,
				SrcPos:   pos,
				object:   expr,
				method:   memberName,
				args:     args,
			}
		} else {
			// Field access: $.field
			expr = &fieldAccessNode{
				nodeType: nodeFieldAccess,
				SrcPos:   pos,
				object:   expr,
				field:    memberName,
			}
		}
	}
	return expr
}

func (p *parser) parseBoolExpr(val bool) node {
	pos := p.token.pos
	p.next()
	return &boolNode{nodeType: nodeBool, SrcPos: pos, val: val}
}

func (p *parser) parseNilExpr() node {
	pos := p.token.pos
	p.next()
	return &nilNode{nodeType: nodeNil, SrcPos: pos}
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
	raw := p.token.val
	val, err := strconv.ParseFloat(raw, 64)
	p.next()
	if err != nil {
		return Error(p.token, "invalid number")
	}
	return &numberNode{
		nodeType: nodeNumber,
		SrcPos:   pos,
		val:      val,
		isInt:    !strings.Contains(raw, "."),
	}
}

func Error(t token, str string) node {
	fmt.Fprintf(os.Stderr, "Error at %s: %s\n\tgot: %s (%q)\n", t.pos, str, t.kind, t.val)
	return nil
}

func ErrorV(str string) llvm.Value {
	fmt.Fprintf(os.Stderr, "Error: %s\n", str)
	return llvm.Value{nil}
}
