package registry_test

import (
	"errors"
	"testing"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/registry"
)

// testUser is a well-formed entity used across registry tests.
type testUser struct {
	ID    string   `version:"id"`
	Name  string   `version:"tracked"`
	Email string   `version:"tracked,redactable"`
	Notes string   `version:"ignore"`
	Tags  []string `version:"normalized"`
}

// TestRegistry_Register verifies that registering a valid entity succeeds
// and the entity can be retrieved by type name.
func TestRegistry_Register(t *testing.T) {
	r := registry.New()

	if err := r.Register(&testUser{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg, err := r.Get("testuser")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if cfg.TypeName != "testuser" {
		t.Errorf("TypeName = %q, want %q", cfg.TypeName, "testuser")
	}
	if cfg.IDField != "ID" {
		t.Errorf("IDField = %q, want %q", cfg.IDField, "ID")
	}
	if len(cfg.Fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(cfg.Fields))
	}
}

// TestRegistry_FieldAnnotations verifies that all 5 tag types are parsed correctly.
func TestRegistry_FieldAnnotations(t *testing.T) {
	r := registry.New()
	_ = r.Register(&testUser{})

	cfg, _ := r.Get("testuser")

	// Build field map for easy lookup.
	fields := make(map[string]registry.FieldConfig)
	for _, f := range cfg.Fields {
		fields[f.Name] = f
	}

	// ID field.
	if !fields["ID"].ID {
		t.Error("ID field should have ID=true")
	}

	// Tracked field.
	if !fields["Name"].Tracked {
		t.Error("Name field should have Tracked=true")
	}

	// Tracked + Redactable (comma-separated).
	email := fields["Email"]
	if !email.Tracked || !email.Redactable {
		t.Errorf("Email: Tracked=%v, Redactable=%v; want both true", email.Tracked, email.Redactable)
	}

	// Ignore field.
	if !fields["Notes"].Ignore {
		t.Error("Notes field should have Ignore=true")
	}

	// Normalized field.
	if !fields["Tags"].Normalized {
		t.Error("Tags field should have Normalized=true")
	}

	// HasTracked should be true.
	if !cfg.HasTracked {
		t.Error("HasTracked should be true")
	}
}

// TestRegistry_MissingIDField verifies that registering an entity without
// a version:"id" field returns ErrConfiguration.
func TestRegistry_MissingIDField(t *testing.T) {
	type NoID struct {
		Name  string `version:"tracked"`
		Email string
	}

	r := registry.New()
	err := r.Register(&NoID{})
	if err == nil {
		t.Fatal("expected error for missing ID field")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

// TestRegistry_DuplicateRegistration verifies that registering the same
// entity type twice returns ErrConfiguration.
func TestRegistry_DuplicateRegistration(t *testing.T) {
	r := registry.New()

	if err := r.Register(&testUser{}); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err := r.Register(&testUser{})
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

// TestRegistry_Get_NotRegistered verifies Get returns ErrConfiguration for
// an unregistered type.
func TestRegistry_Get_NotRegistered(t *testing.T) {
	r := registry.New()

	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for unregistered type")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

// TestRegistry_NonStructInput verifies Register rejects non-struct input.
func TestRegistry_NonStructInput(t *testing.T) {
	r := registry.New()

	err := r.Register("hello")
	if err == nil {
		t.Fatal("expected error for non-struct input")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

// TestRegistry_EntityTypeName verifies that type names are lowercased struct names.
func TestRegistry_EntityTypeName(t *testing.T) {
	type UserProfile struct {
		ID string `version:"id"`
	}

	r := registry.New()
	_ = r.Register(&UserProfile{})

	cfg, err := r.Get("userprofile")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cfg.TypeName != "userprofile" {
		t.Errorf("TypeName = %q, want %q", cfg.TypeName, "userprofile")
	}
}

// TestRegistry_RegisteredTypes verifies the list of registered types.
func TestRegistry_RegisteredTypes(t *testing.T) {
	type A struct {
		ID string `version:"id"`
	}
	type B struct {
		ID string `version:"id"`
	}

	r := registry.New()
	_ = r.Register(&A{})
	_ = r.Register(&B{})

	types := r.RegisteredTypes()
	if len(types) != 2 {
		t.Fatalf("expected 2 types, got %d", len(types))
	}

	typeSet := make(map[string]bool)
	for _, name := range types {
		typeSet[name] = true
	}
	if !typeSet["a"] || !typeSet["b"] {
		t.Errorf("expected types [a, b], got %v", types)
	}
}

// TestRegistry_MultipleIDFields verifies that multiple version:"id" fields is an error.
func TestRegistry_MultipleIDFields(t *testing.T) {
	type TwoIDs struct {
		ID1 string `version:"id"`
		ID2 string `version:"id"`
	}

	r := registry.New()
	err := r.Register(&TwoIDs{})
	if err == nil {
		t.Fatal("expected error for multiple ID fields")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

// TestRegistry_ValueReceiver verifies Register works with value (non-pointer).
func TestRegistry_ValueReceiver(t *testing.T) {
	type Val struct {
		ID string `version:"id"`
	}

	r := registry.New()
	err := r.Register(Val{})
	if err != nil {
		t.Fatalf("Register with value: %v", err)
	}

	_, err = r.Get("val")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GA-014: Entity Guardrails
// ---------------------------------------------------------------------------

// mockLogger captures log messages for test assertions.
type mockLogger struct {
	warns []string
}

func (m *mockLogger) Info(_ string, _ ...any)  {}
func (m *mockLogger) Debug(_ string, _ ...any) {}
func (m *mockLogger) Error(_ string, _ ...any) {}
func (m *mockLogger) Warn(msg string, _ ...any) {
	m.warns = append(m.warns, msg)
}

// TestGuardrails_WarnOnByteSlice verifies that a []byte field without
// version:"ignore" produces a warning (logged, not an error).
func TestGuardrails_WarnOnByteSlice(t *testing.T) {
	type WithBytes struct {
		ID   string `version:"id"`
		Data []byte
	}

	logger := &mockLogger{}
	r := registry.New(registry.WithGuardrails(registry.GuardrailConfig{
		Logger: logger,
	}))

	err := r.Register(&WithBytes{})
	if err != nil {
		t.Fatalf("expected no error (just warning), got: %v", err)
	}

	if len(logger.warns) == 0 {
		t.Fatal("expected a warning for []byte field")
	}
	if len(logger.warns) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(logger.warns), logger.warns)
	}
}

// TestGuardrails_ByteSlice_IgnoredOK verifies that a []byte field WITH
// version:"ignore" does NOT produce a warning.
func TestGuardrails_ByteSlice_IgnoredOK(t *testing.T) {
	type WithIgnoredBytes struct {
		ID   string `version:"id"`
		Data []byte `version:"ignore"`
	}

	logger := &mockLogger{}
	r := registry.New(registry.WithGuardrails(registry.GuardrailConfig{
		Logger: logger,
	}))

	err := r.Register(&WithIgnoredBytes{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logger.warns) != 0 {
		t.Errorf("expected no warnings for ignored []byte, got %d", len(logger.warns))
	}
}

// TestGuardrails_RejectOversizedEntity verifies that an entity serialized
// to >MaxEntityBytes is rejected with EntityTooLargeError.
func TestGuardrails_RejectOversizedEntity(t *testing.T) {
	type Large struct {
		ID   string `version:"id"`
		Blob string
	}

	// Create an entity that serializes to >100 bytes (our test limit).
	bigBlob := make([]byte, 200)
	for i := range bigBlob {
		bigBlob[i] = 'x'
	}
	entity := Large{ID: "1", Blob: string(bigBlob)}

	r := registry.New(registry.WithGuardrails(registry.GuardrailConfig{
		MaxEntityBytes: 100,
	}))

	err := r.Register(entity)
	if err == nil {
		t.Fatal("expected error for oversized entity")
	}

	var tooLarge *domain.EntityTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected EntityTooLargeError, got: %v", err)
	}
	if tooLarge.EntityType != "large" {
		t.Errorf("EntityType = %q, want %q", tooLarge.EntityType, "large")
	}
	if tooLarge.Limit != 100 {
		t.Errorf("Limit = %d, want 100", tooLarge.Limit)
	}
}

// TestGuardrails_StrictMode verifies that StrictRegistration=true turns
// warnings (e.g. unignored []byte) into errors.
func TestGuardrails_StrictMode(t *testing.T) {
	type WithBytes struct {
		ID   string `version:"id"`
		Data []byte
	}

	r := registry.New(registry.WithGuardrails(registry.GuardrailConfig{
		StrictRegistration: true,
	}))

	err := r.Register(&WithBytes{})
	if err == nil {
		t.Fatal("expected error in strict mode for []byte field")
	}
	if !errors.Is(err, domain.ErrConfiguration) {
		t.Errorf("expected ErrConfiguration, got: %v", err)
	}
}

// TestGuardrails_NoGuardrails verifies that without WithGuardrails option,
// no guardrail checks run.
func TestGuardrails_NoGuardrails(t *testing.T) {
	type WithBytes struct {
		ID   string `version:"id"`
		Data []byte
	}

	r := registry.New() // no guardrails
	err := r.Register(&WithBytes{})
	if err != nil {
		t.Fatalf("expected no error without guardrails, got: %v", err)
	}
}
