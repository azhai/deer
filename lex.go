package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

type token struct {
	kind tokenType
	pos  SrcPos
	val  string
}

func (t token) String() string {
	switch {
	case t.kind == tokError:
		return fmt.Sprintf("error: %s", t.val)
	case t.kind == tokEOF:
		return "EOF"
	case t.kind == tokNewFile:
		return fmt.Sprintf("file: %s", t.val)
	case t.kind > tokKeyword:
		return fmt.Sprintf("<%s>", t.val)
	case t.kind == tokSpace:
		return " "
	case len(t.val) > 20:
		return fmt.Sprintf("%.20q...", t.val)
	default:
		return t.val
	}
}

type tokenType int

const (
	tokEndOfTokens tokenType = iota
	tokError
	tokEOF
	tokNewFile
	tokComment
	tokSpace
	tokSemicolon
	tokColon
	tokComma
	tokLeftParen
	tokRightParen
	tokLBrace
	tokRBrace
	tokDot
	tokDollar
	tokNumber
	tokIdentifier
	tokKeyword
	tokDefine
	tokExtern
	tokIf
	tokThen
	tokElse
	tokFor
	tokIn
	tokBinary
	tokUnary
	tokVariable
	tokStruct
	tokNil
	tokTrue
	tokFalse
	tokTypeVoid
	tokTypeBool
	tokTypeByte
	tokTypeShort
	tokTypeRune
	tokTypeInt
	tokTypeFloat
	tokUserUnaryOp
	tokUserBinaryOp
	tokQuotedOp  // `op` quoted operator name
	tokStringLit // 'string literal'
	tokEqual
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokPercent
	tokLessThan
	tokGreaterThan
	tokAmp
	tokPipe
)

var tokenNames = map[tokenType]string{
	tokEndOfTokens: "EndOfTokens",
	tokError:       "Error",
	tokEOF:         "EOF",
	tokNewFile:     "NewFile",
	tokComment:     "Comment",
	tokSpace:       "Space",
	tokSemicolon:   "Semicolon",
	tokColon:       "Colon",
	tokComma:       "Comma",
	tokLeftParen:   "LeftParen",
	tokRightParen:  "RightParen",
	tokLBrace:      "LBrace",
	tokRBrace:      "RBrace",
	tokDot:         "Dot",
	tokDollar:      "Dollar",
	tokNumber:      "Number",
	tokIdentifier:  "Identifier",
	tokDefine:      "def",
	tokExtern:      "extern",
	tokIf:          "if",
	tokThen:        "then",
	tokElse:        "else",
	tokFor:         "for",
	tokIn:          "in",
	tokBinary:      "binary",
	tokUnary:       "unary",
	tokVariable:    "var",
	tokStruct:      "struct",
	tokNil:         "nil",
	tokTrue:        "true",
	tokFalse:       "false",
	tokTypeVoid:    "void",
	tokTypeBool:    "bool",
	tokTypeByte:    "byte",
	tokTypeShort:   "short",
	tokTypeRune:    "rune",
	tokTypeInt:     "int",
	tokTypeFloat:   "float",
	tokStringLit:   "StringLit",
	tokEqual:       "=",
	tokPlus:        "+",
	tokMinus:       "-",
	tokStar:        "*",
	tokSlash:       "/",
	tokPercent:     "%",
	tokLessThan:    "<",
	tokGreaterThan: ">",
	tokAmp:         "&",
	tokPipe:        "|",
}

func (t tokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("TokenType(%d)", t)
}

var key = map[string]tokenType{
	"def":    tokDefine,
	"extern": tokExtern,
	"if":     tokIf,
	"then":   tokThen,
	"else":   tokElse,
	"for":    tokFor,
	"in":     tokIn,
	"binary": tokBinary,
	"unary":  tokUnary,
	"var":    tokVariable,
	"struct": tokStruct,
	"nil":    tokNil,
	"true":   tokTrue,
	"false":  tokFalse,
}

var op = map[rune]tokenType{
	'=': tokEqual,
	'+': tokPlus,
	'-': tokMinus,
	'*': tokStar,
	'/': tokSlash,
	'%': tokPercent,
	'<': tokLessThan,
	'>': tokGreaterThan,
	'&': tokAmp,
	'|': tokPipe,
}

type userOpType int

const (
	uopNOP userOpType = iota
	uopUnaryOp
	uopBinaryOp
)

type stateFn func(*lexer) stateFn

type lexer struct {
	files         chan *os.File
	scanner       *bufio.Scanner
	name          string
	line          string
	state         stateFn
	pos           int
	start         int
	width         int
	lineCount     int
	colStart      int
	parenDepth    int
	tokens        chan token
	userOperators map[rune]userOpType
	opMu          sync.Mutex // protects userOperators
}

func Lex() *lexer {
	l := &lexer{
		files:         make(chan *os.File, 10),
		tokens:        make(chan token, 10),
		userOperators: map[rune]userOpType{},
	}
	go l.run()
	return l
}

// RegisterBinaryOp registers a single-char binary operator so the lexer
// emits it as tokUserBinaryOp instead of its default token kind.
func (l *lexer) RegisterBinaryOp(r rune) {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	l.userOperators[r] = uopBinaryOp
}

// RegisterUnaryOp registers a single-char unary operator.
func (l *lexer) RegisterUnaryOp(r rune) {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	l.userOperators[r] = uopUnaryOp
}

func (l *lexer) isUserBinaryOp(r rune) bool {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	return l.userOperators[r] == uopBinaryOp
}

func (l *lexer) isUserUnaryOp(r rune) bool {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	return l.userOperators[r] == uopUnaryOp
}

func (l *lexer) Add(f *os.File) {
	l.files <- f
}

func (l *lexer) Done() {
	close(l.files)
}

func (l *lexer) Tokens() <-chan token {
	return l.tokens
}

const eof = -1

func (l *lexer) word() string {
	return l.line[l.start:l.pos]
}

func (l *lexer) next() rune {
	if l.pos >= len(l.line) {
		if l.scanner.Scan() {
			l.line = l.scanner.Text() + "\n"
			l.pos = 0
			l.start = 0
			l.width = 0
			l.colStart = 0
			l.lineCount++
		} else {
			l.width = 0
			return eof
		}
	}
	r, w := utf8.DecodeRuneInString(l.line[l.pos:])
	l.width = w
	l.pos += w
	return r
}

func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

func (l *lexer) backup() {
	l.pos -= l.width
}

func (l *lexer) ignore() {
	l.start = l.pos
	l.colStart = l.pos
}

func (l *lexer) acceptRun(valid string) {
	for strings.IndexRune(valid, l.next()) >= 0 {
	}
	l.backup()
}

func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	l.tokens <- token{
		kind: tokError,
		pos:  l.srcPos(),
		val:  fmt.Sprintf(format, args...),
	}
	return nil
}

func (l *lexer) srcPos() SrcPos {
	return SrcPos{
		File: l.name,
		Line: l.lineCount,
		Col:  l.start - l.colStart + 1,
		Pos:  l.start,
	}
}

func (l *lexer) emit(tt tokenType) {
	l.tokens <- token{
		kind: tt,
		pos:  l.srcPos(),
		val:  l.word(),
	}
	l.start = l.pos
	l.colStart = l.pos
}

func (l *lexer) run() {
	for {
		f, ok := <-l.files
		if !ok {
			close(l.tokens)
			return
		}

		l.name = f.Name()
		l.scanner = bufio.NewScanner(f)
		l.line = ""
		l.pos = 0
		l.start = 0
		l.width = 0
		l.lineCount = 0
		l.colStart = 0
		l.parenDepth = 0

		l.tokens <- token{
			kind: tokNewFile,
			pos: SrcPos{
				File: l.name,
				Line: 1,
				Col:  1,
			},
			val: l.name,
		}

		for l.state = lexTopLevel; l.state != nil; {
			l.state = l.state(l)
		}

		f.Close()
	}
}

func lexTopLevel(l *lexer) stateFn {
	r := l.next()
	switch {
	case r == eof:
		return nil
	case isSpace(r):
		l.backup()
		return lexSpace
	case isEOL(r):
		l.start = l.pos
		l.colStart = l.pos
		return lexTopLevel
	case r == ';':
		l.emit(tokSemicolon)
		return lexTopLevel
	case r == ',':
		l.emit(tokComma)
		return lexTopLevel
	case r == '#':
		return lexComment
	case r == '(':
		l.parenDepth++
		l.emit(tokLeftParen)
		return lexTopLevel
	case r == ')':
		l.parenDepth--
		l.emit(tokRightParen)
		if l.parenDepth < 0 {
			return l.errorf("unexpected right paren")
		}
		return lexTopLevel
	case r == '{':
		l.emit(tokLBrace)
		return lexTopLevel
	case r == '}':
		l.emit(tokRBrace)
		return lexTopLevel
	case r == '$':
		l.emit(tokDollar)
		return lexTopLevel
	case r == '.':
		// If next char is a digit, lex as number; otherwise emit dot.
		r2 := l.peek()
		if r2 >= '0' && r2 <= '9' {
			l.backup()
			return lexNumber
		}
		l.emit(tokDot)
		return lexTopLevel
	case '0' <= r && r <= '9':
		l.backup()
		return lexNumber
	case isAlphaNumeric(r):
		l.backup()
		return lexIdentifier
	case op[r] > tokUserBinaryOp:
		l.emit(op[r])
		return lexTopLevel
	case l.isUserBinaryOp(r):
		l.emit(tokUserBinaryOp)
		return lexTopLevel
	case l.isUserUnaryOp(r):
		l.emit(tokUserUnaryOp)
		return lexTopLevel
	case r == '`':
		return lexQuotedOp
	case r == '\'':
		return lexStringLit
	case r == ':':
		l.emit(tokColon)
		return lexTopLevel
	default:
		return l.errorf("unrecognized character: %#U", r)
	}
}

func lexSpace(l *lexer) stateFn {
	globWhitespace(l)
	return lexTopLevel
}

func globWhitespace(l *lexer) {
	for isSpace(l.next()) {
	}
	l.backup()
	if l.start != l.pos {
		l.emit(tokSpace)
	}
}

func lexComment(l *lexer) stateFn {
	l.pos = len(l.line)
	l.emit(tokComment)
	return lexTopLevel
}

func lexNumber(l *lexer) stateFn {
	l.acceptRun("0123456789.xabcdefABCDEF")
	l.emit(tokNumber)
	return lexTopLevel
}

func lexIdentifier(l *lexer) stateFn {
	for {
		switch r := l.next(); {
		case isAlphaNumeric(r):
		default:
			l.backup()
			word := l.word()
			if key[word] > tokKeyword {
				l.emit(key[word])
			} else {
				l.emit(tokIdentifier)
			}
			return lexTopLevel
		}
	}
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}

func isEOL(r rune) bool {
	return r == '\n' || r == '\r' || r == eof
}

func isAlphaNumeric(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// lexQuotedOp reads a backtick-quoted operator name (e.g. `+`, `!`).
// The opening backtick has already been consumed by lexTopLevel.
func lexQuotedOp(l *lexer) stateFn {
	// l.start still points before the opening backtick; advance it past the
	// backtick so l.word() returns only the operator name (e.g. ":" not "`:`").
	l.start = l.pos
	l.colStart = l.pos
	for {
		r := l.next()
		if r == '`' {
			// l.pos is just past the closing backtick; trim it from the word
			// by backing up the emit boundary.
			word := l.line[l.start : l.pos-1]
			l.tokens <- token{
				kind: tokQuotedOp,
				pos:  l.srcPos(),
				val:  word,
			}
			l.start = l.pos
			l.colStart = l.pos
			return lexTopLevel
		}
		if r == eof || isEOL(r) {
			return l.errorf("unterminated quoted operator, expected '`'")
		}
	}
}

// lexStringLit lexes a single-quoted string literal: 'hello world'.
// The opening quote has already been consumed. Supports \' and \\ escapes.
func lexStringLit(l *lexer) stateFn {
	l.start = l.pos
	l.colStart = l.pos
	var buf strings.Builder
	for {
		r := l.next()
		switch {
		case r == '\'':
			l.tokens <- token{
				kind: tokStringLit,
				pos:  l.srcPos(),
				val:  buf.String(),
			}
			l.start = l.pos
			l.colStart = l.pos
			return lexTopLevel
		case r == '\\':
			next := l.next()
			switch next {
			case 'n':
				buf.WriteByte('\n')
			case 't':
				buf.WriteByte('\t')
			case 'r':
				buf.WriteByte('\r')
			case '\\':
				buf.WriteByte('\\')
			case '\'':
				buf.WriteByte('\'')
			case '0':
				buf.WriteByte(0)
			default:
				buf.WriteRune(next)
			}
		case r == eof || isEOL(r):
			return l.errorf("unterminated string literal, expected \"'\"")
		default:
			buf.WriteRune(r)
		}
	}
}
