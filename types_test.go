package main

import "testing"

func TestTypeKindString(t *testing.T) {
	cases := []struct {
		k    TypeKind
		want string
	}{
		{TypeVoid, "void"},
		{TypeBool, "bool"},
		{TypeByte, "byte"},
		{TypeShort, "short"},
		{TypeRune, "rune"},
		{TypeInt, "int"},
		{TypeFloat, "float"},
		{TypeStr, "str"},
		{TypeStruct, "struct"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("TypeKind(%d).String() = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestTypeKindStringUnknown(t *testing.T) {
	k := TypeKind(999)
	if got := k.String(); got != "TypeKind(999)" {
		t.Errorf("unknown TypeKind.String() = %q, want 'TypeKind(999)'", got)
	}
}

func TestBuiltinType(t *testing.T) {
	cases := []struct {
		name string
		want TypeKind
		ok   bool
	}{
		{"void", TypeVoid, true},
		{"bool", TypeBool, true},
		{"byte", TypeByte, true},
		{"short", TypeShort, true},
		{"rune", TypeRune, true},
		{"int", TypeInt, true},
		{"float", TypeFloat, true},
		{"str", TypeStr, true},
		{"unknown", TypeVoid, false},
		{"", TypeVoid, false},
	}
	for _, c := range cases {
		k, ok := builtinType(c.name)
		if ok != c.ok {
			t.Errorf("builtinType(%q): ok = %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && k != c.want {
			t.Errorf("builtinType(%q): kind = %v, want %v", c.name, k, c.want)
		}
	}
}

func TestIsTypeKeyword(t *testing.T) {
	typeNames := []string{"void", "bool", "byte", "short", "rune", "int", "float", "str"}
	for _, name := range typeNames {
		if !isTypeKeyword(name) {
			t.Errorf("isTypeKeyword(%q) = false, want true", name)
		}
	}
	if isTypeKeyword("notatype") {
		t.Error("isTypeKeyword('notatype') = true, want false")
	}
}

func TestIsIntegerType(t *testing.T) {
	intTypes := []TypeKind{TypeBool, TypeByte, TypeShort, TypeRune, TypeInt}
	for _, k := range intTypes {
		if !isIntegerType(k) {
			t.Errorf("isIntegerType(%v) = false, want true", k)
		}
	}
	nonIntTypes := []TypeKind{TypeFloat, TypeStr, TypeStruct, TypeVoid}
	for _, k := range nonIntTypes {
		if isIntegerType(k) {
			t.Errorf("isIntegerType(%v) = true, want false", k)
		}
	}
}

func TestIsNumericType(t *testing.T) {
	numericTypes := []TypeKind{TypeBool, TypeByte, TypeShort, TypeRune, TypeInt, TypeFloat}
	for _, k := range numericTypes {
		if !isNumericType(k) {
			t.Errorf("isNumericType(%v) = false, want true", k)
		}
	}
	nonNumeric := []TypeKind{TypeStr, TypeStruct, TypeVoid}
	for _, k := range nonNumeric {
		if isNumericType(k) {
			t.Errorf("isNumericType(%v) = true, want false", k)
		}
	}
}

func TestStructRegistry(t *testing.T) {
	// Save and restore the global registry to avoid cross-test interference.
	origRegistry := structRegistry
	defer func() { structRegistry = origRegistry }()

	structRegistry = map[string]*StructDef{}

	def := &StructDef{
		Name: "Point",
		Fields: []StructField{
			{Name: "x", Type: TypeInt},
			{Name: "y", Type: TypeInt},
		},
		Methods: map[string]*fnPrototypeNode{},
	}
	registerStruct(def)

	got := lookupStruct("Point")
	if got == nil {
		t.Fatal("lookupStruct('Point') = nil, want non-nil")
	}
	if got != def {
		t.Error("lookupStruct returned a different pointer than registered")
	}

	if lookupStruct("Unknown") != nil {
		t.Error("lookupStruct('Unknown') = non-nil, want nil")
	}
}

func TestStructDefFieldIndex(t *testing.T) {
	def := &StructDef{
		Name: "Color",
		Fields: []StructField{
			{Name: "r", Type: TypeByte},
			{Name: "g", Type: TypeByte},
			{Name: "b", Type: TypeByte},
		},
	}
	cases := []struct {
		field string
		want  int
	}{
		{"r", 0},
		{"g", 1},
		{"b", 2},
		{"a", -1},
		{"", -1},
	}
	for _, c := range cases {
		if got := def.fieldIndex(c.field); got != c.want {
			t.Errorf("fieldIndex(%q) = %d, want %d", c.field, got, c.want)
		}
	}
}

func TestStructFieldPrivate(t *testing.T) {
	f := StructField{Name: "_id", Type: TypeInt, Private: true}
	if !f.Private {
		t.Error("Private = false, want true")
	}
	f = StructField{Name: "id", Type: TypeInt, Private: false}
	if f.Private {
		t.Error("Private = true, want false")
	}
}
