package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
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
	tokUserUnaryOp
	tokUserBinaryOp
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
	case '0' <= r && r <= '9', r == '.':
		l.backup()
		return lexNumber
	case isAlphaNumeric(r):
		l.backup()
		return lexIdentifier
	case op[r] > tokUserBinaryOp:
		l.emit(op[r])
		return lexTopLevel
	case l.userOperators[r] == uopBinaryOp:
		l.emit(tokUserBinaryOp)
		return lexTopLevel
	case l.userOperators[r] == uopUnaryOp:
		l.emit(tokUserUnaryOp)
		return lexTopLevel
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
				switch word {
				case "binary":
					return lexUserBinaryOp
				case "unary":
					return lexUserUnaryOp
				}
			} else {
				l.emit(tokIdentifier)
			}
			return lexTopLevel
		}
	}
}

func lexUserBinaryOp(l *lexer) stateFn {
	globWhitespace(l)
	r := l.next()
	l.userOperators[r] = uopBinaryOp
	l.emit(tokUserBinaryOp)
	return lexTopLevel
}

func lexUserUnaryOp(l *lexer) stateFn {
	globWhitespace(l)
	r := l.next()
	l.userOperators[r] = uopUnaryOp
	l.emit(tokUserUnaryOp)
	return lexTopLevel
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
