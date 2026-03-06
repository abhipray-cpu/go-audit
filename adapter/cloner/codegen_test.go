package cloner

import (
	"context"
	"strings"
	"testing"
)

func TestCodegenCloner_DeepCopy(t *testing.T) {
	src, err := Generate(GenerateOptions{
		PackageName: "myapp",
		TypeName:    "User",
		Fields: []FieldSpec{
			{Name: "ID", Type: "string"},
			{Name: "Name", Type: "string"},
			{Name: "Tags", Type: "[]string"},
			{Name: "Settings", Type: "map[string]string"},
			{Name: "Score", Type: "int"},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	code := string(src)

	// Verify package declaration.
	if !strings.Contains(code, "package myapp") {
		t.Errorf("expected 'package myapp' in generated code")
	}

	// Verify type name.
	if !strings.Contains(code, "UserCloner") {
		t.Errorf("expected 'UserCloner' struct in generated code")
	}

	// Verify Clone method.
	if !strings.Contains(code, "func (c *UserCloner) Clone") {
		t.Errorf("expected Clone method on UserCloner")
	}

	// Verify slice deep copy (make + copy).
	if !strings.Contains(code, "make([]string, len(src.Tags))") {
		t.Errorf("expected slice deep copy for Tags")
	}

	// Verify map deep copy.
	if !strings.Contains(code, "make(map[string]string, len(src.Settings))") {
		t.Errorf("expected map deep copy for Settings")
	}

	// Verify value type direct copy.
	if !strings.Contains(code, "src.Score") {
		t.Errorf("expected direct copy for Score")
	}

	// Verify no reflection imports.
	if strings.Contains(code, "reflect") {
		t.Errorf("generated code should not import reflect")
	}

	// Verify DO NOT EDIT header.
	if !strings.Contains(code, "DO NOT EDIT") {
		t.Errorf("expected DO NOT EDIT header")
	}
}

func TestCodegenCloner_PointerField(t *testing.T) {
	src, err := Generate(GenerateOptions{
		PackageName: "pkg",
		TypeName:    "Order",
		Fields: []FieldSpec{
			{Name: "ID", Type: "string"},
			{Name: "Note", Type: "*string"},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	code := string(src)
	if !strings.Contains(code, "*src.Note") {
		t.Errorf("expected pointer dereference for Note field")
	}
}

func TestCodegenCloner_EmptyFields(t *testing.T) {
	src, err := Generate(GenerateOptions{
		PackageName: "pkg",
		TypeName:    "Empty",
		Fields:      nil,
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	code := string(src)
	if !strings.Contains(code, "EmptyCloner") {
		t.Errorf("expected EmptyCloner in generated code")
	}
}

func TestCodegenCloner_MissingPackage(t *testing.T) {
	_, err := Generate(GenerateOptions{TypeName: "Foo"})
	if err == nil {
		t.Fatal("expected error for missing PackageName")
	}
}

func TestCodegenCloner_MissingType(t *testing.T) {
	_, err := Generate(GenerateOptions{PackageName: "pkg"})
	if err == nil {
		t.Fatal("expected error for missing TypeName")
	}
}

// BenchmarkClone_Codegen_vs_Reflect compares generated clone vs reflect clone.
func BenchmarkClone_Codegen_vs_Reflect(b *testing.B) {
	type BenchUser struct {
		ID    string
		Name  string
		Email string
		Tags  []string
	}

	user := &BenchUser{
		ID:    "u1",
		Name:  "Alice",
		Email: "alice@example.com",
		Tags:  []string{"admin", "active"},
	}

	b.Run("Reflect", func(b *testing.B) {
		c := NewReflect()
		ctx := context.Background()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := c.Clone(ctx, user)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Codegen_Simulated", func(b *testing.B) {
		// Simulate what generated code does — direct field copy without reflection.
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tags := make([]string, len(user.Tags))
			copy(tags, user.Tags)
			clone := &BenchUser{
				ID:    user.ID,
				Name:  user.Name,
				Email: user.Email,
				Tags:  tags,
			}
			_ = clone
		}
	})

	// Verify generation works.
	src, err := Generate(GenerateOptions{
		PackageName: "bench",
		TypeName:    "BenchUser",
		Fields: []FieldSpec{
			{Name: "ID", Type: "string"},
			{Name: "Name", Type: "string"},
			{Name: "Email", Type: "string"},
			{Name: "Tags", Type: "[]string"},
		},
	})
	if err != nil {
		b.Fatalf("Generate: %v", err)
	}
	if len(src) == 0 {
		b.Fatal("empty generated source")
	}

	b.Logf("generated %d bytes", len(src))
}
