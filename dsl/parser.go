package dsl

import (
	"fmt"
	"io"
	"strconv"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

var (
	dslLexer = lexer.MustSimple([]lexer.SimpleRule{
		{Name: "Whitespace", Pattern: `[ \t\r]+`},
		{Name: "Newline", Pattern: `\n+`},
		{Name: "BlockComment", Pattern: `/\*[^*]*\*+(?:[^/*][^*]*\*+)*/`},
		{Name: "LineComment", Pattern: `//[^\n]*`},
		{Name: "Color", Pattern: `#(?:[0-9A-Fa-f]{3}|[0-9A-Fa-f]{6}|[0-9A-Fa-f]{8})`},
		{Name: "HashComment", Pattern: `#[^\n]*`},
		{Name: "Number", Pattern: `(?:\d+\.\d+|\d+)(?:pt|mm|cm|in|%|x)?`},
		{Name: "String", Pattern: `"(?:\\.|[^"])*"`},
		{Name: "Ident", Pattern: `[A-Za-z_][A-Za-z0-9_-]*`},
		{Name: "Symbol", Pattern: `[][(),.=+\-*/%<>!?;:]`},
		{Name: "LBrace", Pattern: `{`},
		{Name: "RBrace", Pattern: `}`},
	})

	tokenNames       = invertSymbols(dslLexer.Symbols())
	newlineTokenType = mustTokenType("Newline")
	lbraceTokenType  = mustTokenType("LBrace")
	rbraceTokenType  = mustTokenType("RBrace")
	symbolTokenType  = mustTokenType("Symbol")
	stringTokenType  = mustTokenType("String")

	documentParser = participle.MustBuild[Document](
		participle.Lexer(dslLexer),
		participle.Elide("Whitespace", "LineComment", "BlockComment", "HashComment"),
	)
)

// Document is the root AST node for a Papyrus DSL file.
type Document struct {
	Pos      lexer.Position `parser:"" json:"-"`
	Name     string         `parser:"Newline* 'doc' @Ident"`
	Version  string         `parser:"@Ident"`
	Sections []*Section     `parser:"'{' Newline* ( @@ Newline* )* '}' Newline*"`
}

// Section represents a top-level section (meta/resources/page-set/page).
type Section struct {
	Meta      *MetaSection      `parser:"  @@"`
	Resources *ResourcesSection `parser:"| @@"`
	PageSet   *PageSetSection   `parser:"| @@"`
	Page      *PageSection      `parser:"| @@"`
}

// Kind returns the human-readable section type.
func (s *Section) Kind() string {
	switch {
	case s == nil:
		return "unknown"
	case s.Meta != nil:
		return "meta"
	case s.Resources != nil:
		return "resources"
	case s.PageSet != nil:
		return "page-set"
	case s.Page != nil:
		return "page"
	default:
		return "unknown"
	}
}

// MetaSection captures metadata assignments.
type MetaSection struct {
	Block *Block `parser:"'meta' @@"`
}

// ResourcesSection groups resource declarations.
type ResourcesSection struct {
	Block *Block `parser:"'resources' @@"`
}

// PageSetSection defines reusable page templates.
type PageSetSection struct {
	Name  string `parser:"'page-set' @Ident"`
	Block *Block `parser:"@@"`
}

// PageSection represents a concrete page description.
type PageSection struct {
	Spec  PageSpec `parser:"'page' @@"`
	Block *Block   `parser:"@@"`
}

// PageSpec stores header tokens (eg: size, orientation).
type PageSpec struct {
	Size   string    `parser:"@Ident"`
	Params []*Lexeme `parser:"@@*"`
}

// Block is a delimited list of statements.
type Block struct {
	Statements []*Statement `parser:"'{' Newline* ( @@ ( ';' | Newline )* )* '}'"`
}

// Statement inside a block (assignment/command/text literal).
type Statement struct {
	Assignment *Assignment  `parser:"  @@"`
	Command    *Command     `parser:"| @@"`
	Text       *TextLiteral `parser:"| @@"`
}

// Assignment uses colon syntax (key: value).
type Assignment struct {
	Key   string `parser:"@Ident"`
	Value *Value `parser:"':' Newline* @@"`
}

// Command describes layout/drawing instructions.
type Command struct {
	Pos   lexer.Position `parser:"" json:"-"`
	Name  string         `parser:"@Ident"`
	Args  []*Lexeme      `parser:"@@*"`
	Block *Block         `parser:"( Newline* @@ )?"`
}

// TextLiteral encapsulates raw string statements within blocks.
type TextLiteral struct {
	Value StringLiteral `parser:"@String"`
}

// Value represents generic property values.
type Value struct {
	String *StringLiteral `parser:"  @String"`
	Number *string        `parser:"| @Number"`
	Color  *string        `parser:"| @Color"`
	Array  *ArrayValue    `parser:"| @@"`
	Object *InlineObject  `parser:"| @@"`
	Expr   *Expression    `parser:"| @@"`
}

// ArrayValue captures `[ ... ]` expressions.
type ArrayValue struct {
	Values []*Value `parser:"'[' Newline* ( @@ ( (',' | ';' | Newline+) Newline* @@ )* )? Newline* ']'"`
}

// InlineObject captures `{ key: value }` inline maps.
type InlineObject struct {
	Entries []*Assignment `parser:"'{' Newline* ( @@ Newline* ( (';' | Newline+) Newline* @@ Newline* )* )? Newline* '}'"`
}

// Expression records raw tokens for later evaluation.
type Expression struct {
	Parts []*Lexeme
}

// Parse implements participle.Parseable for Expression.
func (e *Expression) Parse(lex *lexer.PeekingLexer) error {
	var parts []*Lexeme
	var parenDepth int
	var bracketDepth int

	for {
		tok := lex.Peek()
		if tok.EOF() {
			break
		}
		if stopExpression(tok, parenDepth, bracketDepth) {
			break
		}

		lexeme, err := consumeLexeme(lex)
		if err != nil {
			return err
		}
		switch lexeme.Raw {
		case "(":
			parenDepth++
		case ")":
			if parenDepth > 0 {
				parenDepth--
			}
		case "[":
			bracketDepth++
		case "]":
			if bracketDepth > 0 {
				bracketDepth--
			}
		}
		parts = append(parts, lexeme)
	}

	if len(parts) == 0 {
		return participle.NextMatch
	}

	e.Parts = parts
	return nil
}

// Lexeme captures a single lexical token (used by commands/expressions).
type Lexeme struct {
	Type  string         `json:"type"`
	Value string         `json:"value"`
	Raw   string         `json:"raw"`
	Pos   lexer.Position `json:"-"`
}

// Parse implements participle.Parseable so Lexeme can act as a grammar atom.
func (l *Lexeme) Parse(lex *lexer.PeekingLexer) error {
	tok := lex.Peek()
	if shouldStopArg(tok) {
		return participle.NextMatch
	}

	lexeme, err := consumeLexeme(lex)
	if err != nil {
		return err
	}
	*l = *lexeme
	return nil
}

// StringLiteral unquotes Go-style strings on capture.
type StringLiteral string

// Capture implements participle.Capture.
func (s *StringLiteral) Capture(values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("string literal capture requires value")
	}
	val, err := strconv.Unquote(values[0])
	if err != nil {
		return err
	}
	*s = StringLiteral(val)
	return nil
}

// Parse parses DSL content from an io.Reader.
func Parse(r io.Reader) (*Document, error) {
	return documentParser.Parse("", r)
}

// ParseString parses DSL content from a string.
func ParseString(input string) (*Document, error) {
	return documentParser.ParseString("", input)
}

// consumeLexeme reads the next non-terminating token and converts it to a Lexeme.
func consumeLexeme(lex *lexer.PeekingLexer) (*Lexeme, error) {
	tok := lex.Next()
	if tok.EOF() {
		return nil, participle.NextMatch
	}

	lexeme, err := newLexeme(*tok)
	if err != nil {
		return nil, err
	}
	return &lexeme, nil
}

func shouldStopArg(tok *lexer.Token) bool {
	if tok == nil || tok.EOF() {
		return true
	}
	switch tok.Type {
	case newlineTokenType, rbraceTokenType, lbraceTokenType:
		return true
	case symbolTokenType:
		return tok.Value == ";"
	default:
		return false
	}
}

func stopExpression(tok *lexer.Token, parenDepth, bracketDepth int) bool {
	if tok == nil || tok.EOF() {
		return true
	}

	if tok.Type == newlineTokenType && parenDepth == 0 && bracketDepth == 0 {
		return true
	}

	if tok.Type == rbraceTokenType && parenDepth == 0 && bracketDepth == 0 {
		return true
	}

	if tok.Type == lbraceTokenType && parenDepth == 0 && bracketDepth == 0 {
		return true
	}

	if tok.Type == symbolTokenType {
		switch tok.Value {
		case ";":
			return parenDepth == 0 && bracketDepth == 0
		case ",":
			return parenDepth == 0 && bracketDepth == 0
		case "]":
			return bracketDepth == 0
		}
	}

	return false
}

func newLexeme(tok lexer.Token) (Lexeme, error) {
	name, ok := tokenNames[tok.Type]
	if !ok {
		name = fmt.Sprintf("#%d", tok.Type)
	}
	val := tok.Value
	if tok.Type == stringTokenType {
		unquoted, err := strconv.Unquote(tok.Value)
		if err != nil {
			return Lexeme{}, err
		}
		val = unquoted
	}

	return Lexeme{
		Type:  name,
		Value: val,
		Raw:   tok.Value,
		Pos:   tok.Pos,
	}, nil
}

func invertSymbols(symbols map[string]lexer.TokenType) map[lexer.TokenType]string {
	out := make(map[lexer.TokenType]string, len(symbols))
	for name, tt := range symbols {
		out[tt] = name
	}
	return out
}

func mustTokenType(name string) lexer.TokenType {
	symbols := dslLexer.Symbols()
	tt, ok := symbols[name]
	if !ok {
		panic(fmt.Sprintf("token %s not defined", name))
	}
	return tt
}
