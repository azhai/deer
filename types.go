package main

import "fmt"

// TypeKind represents the type of a value in Deer.
type TypeKind int

const (
	TypeVoid TypeKind = iota
	TypeBool
	TypeByte  // uint8
	TypeShort // uint16
	TypeRune  // int32
	TypeInt   // int64
	TypeFloat // float64
	TypeStr   // string
	TypeStruct
)

var typeKindNames = map[TypeKind]string{
	TypeVoid:   "void",
	TypeBool:   "bool",
	TypeByte:   "byte",
	TypeShort:  "short",
	TypeRune:   "rune",
	TypeInt:    "int",
	TypeFloat:  "float",
	TypeStr:    "str",
	TypeStruct: "struct",
}

func (t TypeKind) String() string {
	if name, ok := typeKindNames[t]; ok {
		return name
	}
	return fmt.Sprintf("TypeKind(%d)", int(t))
}

// builtinTypeMap maps type name keywords to their TypeKind.
var builtinTypeMap = map[string]TypeKind{
	"void":  TypeVoid,
	"bool":  TypeBool,
	"byte":  TypeByte,
	"short": TypeShort,
	"rune":  TypeRune,
	"int":   TypeInt,
	"float": TypeFloat,
	"str":   TypeStr,
}

// builtinType returns the TypeKind for a builtin type name, or false if not found.
func builtinType(name string) (TypeKind, bool) {
	k, ok := builtinTypeMap[name]
	return k, ok
}

// isTypeKeyword returns true if the name is a builtin type keyword.
func isTypeKeyword(name string) bool {
	_, ok := builtinTypeMap[name]
	return ok
}

// isIntegerType returns true for bool, byte, short, rune, int.
func isIntegerType(k TypeKind) bool {
	return k >= TypeBool && k <= TypeInt
}

// isNumericType returns true for integer types and float.
func isNumericType(k TypeKind) bool {
	return k >= TypeBool && k <= TypeFloat
}

// StructField describes a single field in a struct definition.
type StructField struct {
	Name    string
	Type    TypeKind
	Private bool // _ prefix
}

// StructDef describes a user-defined struct type.
type StructDef struct {
	Name    string
	Parent  string // inherited parent type name (empty if none)
	Fields  []StructField
	Methods map[string]*fnPrototypeNode
}

// structRegistry holds all defined struct types.
var structRegistry = map[string]*StructDef{}

// registerStruct adds a struct definition to the registry.
func registerStruct(def *StructDef) {
	structRegistry[def.Name] = def
}

// lookupStruct returns the struct definition for a name, or nil.
func lookupStruct(name string) *StructDef {
	return structRegistry[name]
}

// fieldIndex returns the index of a field by name, or -1 if not found.
func (s *StructDef) fieldIndex(name string) int {
	for i, f := range s.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}
