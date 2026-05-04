package codegen

import (
	"kaula-compiler/internal/ast"
	"kaula-compiler/internal/config"
	"strings"
	"testing"
)

func testConfig() *config.Config {
	return &config.Config{
		TemplatePath: ".",
		StdlibPath:   "../stdlib.json",
	}
}

func TestGenerateInterfaceStatement_MethodGroup(t *testing.T) {
	cfg := testConfig()
	cg := NewCodeGenerator(cfg)
	tg := NewTypeGenerator(cg)

	iface := &ast.InterfaceStatement{
		Name: "Stringer",
		Methods: []*ast.MethodStatement{
			{
				Name:       "toString",
				Params:     []*ast.Param{},
				ReturnType: "string",
				Body:       []ast.Statement{},
			},
		},
	}

	output := tg.GenerateInterfaceStatement(iface)

	if !strings.Contains(output, "Stringer_MethodGroup") {
		t.Errorf("Expected output to contain 'Stringer_MethodGroup', got:\n%s", output)
	}

	if strings.Contains(output, "Stringer_VTable") {
		t.Errorf("Output should not contain 'Stringer_VTable' (old VTable pattern), got:\n%s", output)
	}

	// 验证 MethodGroup 包含方法指针
	if !strings.Contains(output, "char* (*toString)(void* self)") {
		t.Errorf("Expected interface wrapper function, got:\n%s", output)
	}
}

func TestGenerateClassStatement_ImplementsInterface(t *testing.T) {
	cfg := testConfig()
	cg := NewCodeGenerator(cfg)
	tg := NewTypeGenerator(cg)

	iface := &ast.InterfaceStatement{
		Name: "Comparable",
		Methods: []*ast.MethodStatement{
			{
				Name:       "compare",
				Params:     []*ast.Param{{Name: "other", Type: "Comparable"}},
				ReturnType: "int",
				Body:       []ast.Statement{},
			},
		},
	}

	class := &ast.ClassStatement{
		Name: "MyInt",
		Fields: []*ast.FieldDeclaration{
			{Name: "value", Type: "int"},
		},
		Implements: []string{"Comparable"},
		Constructors: []*ast.ConstructorStatement{
			{
				Params: []*ast.Param{{Name: "v", Type: "int"}},
				Body:   []ast.Statement{},
			},
		},
		Methods: []*ast.MethodStatement{
			{
				Name:       "compare",
				Params:     []*ast.Param{{Name: "other", Type: "Comparable"}},
				ReturnType: "int",
				Body:       []ast.Statement{},
			},
		},
	}

	// Set up the program so getInterfaceMethods can find the interface
	program := &ast.Program{
		Statements: []ast.Statement{iface, class},
	}
	cg.program = program

	var output string
	output += tg.GenerateInterfaceStatement(iface)
	output += tg.GenerateClassStatement(class)

	if !strings.Contains(output, "Comparable_MethodGroup Comparable;") {
		t.Errorf("Expected class to embed Comparable_MethodGroup, got:\n%s", output)
	}

	if !strings.Contains(output, "self->Comparable.compare = MyInt_compare;") {
		t.Errorf("Expected constructor to initialize method group, got:\n%s", output)
	}
}

func TestGenerateClassStatement_MultipleInterfaces(t *testing.T) {
	cfg := testConfig()
	cg := NewCodeGenerator(cfg)
	tg := NewTypeGenerator(cg)

	iface1 := &ast.InterfaceStatement{
		Name: "Stringer",
		Methods: []*ast.MethodStatement{
			{
				Name:       "toString",
				Params:     []*ast.Param{},
				ReturnType: "string",
				Body:       []ast.Statement{},
			},
		},
	}

	iface2 := &ast.InterfaceStatement{
		Name: "Printable",
		Methods: []*ast.MethodStatement{
			{
				Name:       "print",
				Params:     []*ast.Param{},
				ReturnType: "void",
				Body:       []ast.Statement{},
			},
		},
	}

	class := &ast.ClassStatement{
		Name: "Person",
		Fields: []*ast.FieldDeclaration{
			{Name: "name", Type: "string"},
			{Name: "age", Type: "int"},
		},
		Implements: []string{"Stringer", "Printable"},
		Constructors: []*ast.ConstructorStatement{
			{
				Params: []*ast.Param{},
				Body:   []ast.Statement{},
			},
		},
		Methods: []*ast.MethodStatement{
			{
				Name:       "toString",
				Params:     []*ast.Param{},
				ReturnType: "string",
				Body:       []ast.Statement{},
			},
			{
				Name:       "print",
				Params:     []*ast.Param{},
				ReturnType: "void",
				Body:       []ast.Statement{},
			},
		},
	}

	// Set up the program so getInterfaceMethods can find the interfaces
	program := &ast.Program{
		Statements: []ast.Statement{iface1, iface2, class},
	}
	cg.program = program

	var output string
	output += tg.GenerateInterfaceStatement(iface1)
	output += tg.GenerateInterfaceStatement(iface2)
	output += tg.GenerateClassStatement(class)

	if !strings.Contains(output, "Stringer_MethodGroup Stringer;") {
		t.Errorf("Expected class to embed Stringer_MethodGroup, got:\n%s", output)
	}

	if !strings.Contains(output, "Printable_MethodGroup Printable;") {
		t.Errorf("Expected class to embed Printable_MethodGroup, got:\n%s", output)
	}

	if !strings.Contains(output, "self->Stringer.toString = Person_toString;") {
		t.Errorf("Expected constructor to initialize Stringer method group, got:\n%s", output)
	}

	if !strings.Contains(output, "self->Printable.print = Person_print;") {
		t.Errorf("Expected constructor to initialize Printable method group, got:\n%s", output)
	}
}

func TestGenerateClassStatement_NoImplements(t *testing.T) {
	cfg := testConfig()
	cg := NewCodeGenerator(cfg)
	tg := NewTypeGenerator(cg)

	class := &ast.ClassStatement{
		Name: "SimpleClass",
		Fields: []*ast.FieldDeclaration{
			{Name: "x", Type: "int"},
		},
		Implements: nil,
		Constructors: []*ast.ConstructorStatement{
			{
				Params: []*ast.Param{{Name: "val", Type: "int"}},
				Body:   []ast.Statement{},
			},
		},
		Methods: []*ast.MethodStatement{
			{
				Name:       "getX",
				Params:     []*ast.Param{},
				ReturnType: "int",
				Body:       []ast.Statement{},
			},
		},
	}

	output := tg.GenerateClassStatement(class)

	if strings.Contains(output, "MethodGroup") {
		t.Errorf("Class without implements should not contain MethodGroup, got:\n%s", output)
	}

	if !strings.Contains(output, "int x;") {
		t.Errorf("Expected class to have field 'int x;', got:\n%s", output)
	}

	if !strings.Contains(output, "static inline int SimpleClass_getX(SimpleClass* self") {
		t.Errorf("Expected method generation, got:\n%s", output)
	}
}

func TestInterfaceWrapperFunction(t *testing.T) {
	cfg := testConfig()
	cg := NewCodeGenerator(cfg)
	tg := NewTypeGenerator(cg)

	iface := &ast.InterfaceStatement{
		Name: "IOReader",
		Methods: []*ast.MethodStatement{
			{
				Name:       "read",
				Params:     []*ast.Param{{Name: "buf", Type: "char*"}, {Name: "len", Type: "int"}},
				ReturnType: "int",
				Body:       []ast.Statement{},
			},
		},
	}

	output := tg.GenerateInterfaceStatement(iface)

	if !strings.Contains(output, "static inline int IOReader_read(void* self, char* buf, int len)") {
		t.Errorf("Expected interface wrapper function with correct signature, got:\n%s", output)
	}

	if !strings.Contains(output, "IOReader* concrete = (IOReader*)self;") {
		t.Errorf("Expected concrete type cast, got:\n%s", output)
	}
}

func BenchmarkInterfaceComposition(b *testing.B) {
	cfg := testConfig()
	cg := NewCodeGenerator(cfg)
	tg := NewTypeGenerator(cg)

	iface := &ast.InterfaceStatement{
		Name: "BenchmarkIface",
		Methods: []*ast.MethodStatement{
			{Name: "method1", Params: []*ast.Param{{Name: "x", Type: "int"}}, ReturnType: "int"},
			{Name: "method2", Params: []*ast.Param{{Name: "y", Type: "string"}}, ReturnType: "string"},
			{Name: "method3", Params: []*ast.Param{{Name: "a", Type: "int"}, {Name: "b", Type: "int"}}, ReturnType: "int"},
		},
	}

	class := &ast.ClassStatement{
		Name: "BenchmarkClass",
		Fields: []*ast.FieldDeclaration{
			{Name: "field1", Type: "int"},
			{Name: "field2", Type: "string"},
		},
		Implements: []string{"BenchmarkIface"},
		Constructors: []*ast.ConstructorStatement{
			{Params: []*ast.Param{}, Body: []ast.Statement{}},
		},
		Methods: []*ast.MethodStatement{
			{Name: "method1", Params: []*ast.Param{{Name: "x", Type: "int"}}, ReturnType: "int"},
			{Name: "method2", Params: []*ast.Param{{Name: "y", Type: "string"}}, ReturnType: "string"},
			{Name: "method3", Params: []*ast.Param{{Name: "a", Type: "int"}, {Name: "b", Type: "int"}}, ReturnType: "int"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tg.GenerateInterfaceStatement(iface)
		_ = tg.GenerateClassStatement(class)
	}
}
