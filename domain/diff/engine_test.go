package diff_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/diff"
)

// testUser is a simple flat struct used across diff tests.
type testUser struct {
	Name  string
	Email string
	Age   int
}

func TestDiff_FlatStruct_SingleFieldChange(t *testing.T) {
	e := diff.New()

	prev := testUser{Name: "Alice", Email: "alice@example.com", Age: 30}
	curr := testUser{Name: "Alice", Email: "alice@newmail.com", Age: 30}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}

	change := delta.Changes[0]
	if change.Path != "Email" {
		t.Errorf("expected path %q, got %q", "Email", change.Path)
	}

	// Verify OldValue and NewValue are valid JSON-encoded strings.
	var oldVal, newVal string
	if err := json.Unmarshal(change.OldValue, &oldVal); err != nil {
		t.Fatalf("OldValue is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(change.NewValue, &newVal); err != nil {
		t.Fatalf("NewValue is not valid JSON: %v", err)
	}

	if oldVal != "alice@example.com" {
		t.Errorf("expected old value %q, got %q", "alice@example.com", oldVal)
	}
	if newVal != "alice@newmail.com" {
		t.Errorf("expected new value %q, got %q", "alice@newmail.com", newVal)
	}
}

func TestDiff_FlatStruct_MultipleChanges(t *testing.T) {
	e := diff.New()

	prev := testUser{Name: "Alice", Email: "alice@example.com", Age: 30}
	curr := testUser{Name: "Bob", Email: "alice@example.com", Age: 31}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(delta.Changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(delta.Changes))
	}

	// Changes should be in field-declaration order.
	paths := make([]string, len(delta.Changes))
	for i, c := range delta.Changes {
		paths[i] = c.Path
	}

	if paths[0] != "Name" {
		t.Errorf("expected first change path %q, got %q", "Name", paths[0])
	}
	if paths[1] != "Age" {
		t.Errorf("expected second change path %q, got %q", "Age", paths[1])
	}

	// Verify JSON encoding for Name change.
	var oldName, newName string
	if err := json.Unmarshal(delta.Changes[0].OldValue, &oldName); err != nil {
		t.Fatalf("OldValue for Name is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(delta.Changes[0].NewValue, &newName); err != nil {
		t.Fatalf("NewValue for Name is not valid JSON: %v", err)
	}
	if oldName != "Alice" || newName != "Bob" {
		t.Errorf("Name change: expected Alice→Bob, got %s→%s", oldName, newName)
	}

	// Verify JSON encoding for Age change.
	var oldAge, newAge int
	if err := json.Unmarshal(delta.Changes[1].OldValue, &oldAge); err != nil {
		t.Fatalf("OldValue for Age is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(delta.Changes[1].NewValue, &newAge); err != nil {
		t.Fatalf("NewValue for Age is not valid JSON: %v", err)
	}
	if oldAge != 30 || newAge != 31 {
		t.Errorf("Age change: expected 30→31, got %d→%d", oldAge, newAge)
	}
}

func TestDiff_FlatStruct_NoChanges(t *testing.T) {
	e := diff.New()

	user := testUser{Name: "Alice", Email: "alice@example.com", Age: 30}

	delta, err := e.Diff(context.Background(), user, user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta, got %d changes", len(delta.Changes))
	}
}

func TestDiff_CacheCapExceeded(t *testing.T) {
	// Engine with cache cap of 1 — second distinct type should fail.
	e := diff.New(diff.WithMaxCachedTypes(1))

	type typeA struct{ X int }
	type typeB struct{ Y int }

	// First type succeeds and caches.
	_, err := e.Diff(context.Background(), typeA{X: 1}, typeA{X: 2})
	if err != nil {
		t.Fatalf("first type should succeed: %v", err)
	}
	if e.CachedTypes() != 1 {
		t.Fatalf("expected 1 cached type, got %d", e.CachedTypes())
	}

	// Second distinct type should fail with ErrDiff.
	_, err = e.Diff(context.Background(), typeB{Y: 1}, typeB{Y: 2})
	if err == nil {
		t.Fatal("expected error when cache cap exceeded, got nil")
	}
	if !errors.Is(err, domain.ErrDiff) {
		t.Fatalf("expected ErrDiff, got: %v", err)
	}

	// Repeat of first type should still succeed (already cached).
	_, err = e.Diff(context.Background(), typeA{X: 3}, typeA{X: 4})
	if err != nil {
		t.Fatalf("cached type should still work: %v", err)
	}
}

func TestDiff_CachedTypes_Observable(t *testing.T) {
	e := diff.New()

	if e.CachedTypes() != 0 {
		t.Fatalf("new engine should have 0 cached types, got %d", e.CachedTypes())
	}

	_, _ = e.Diff(context.Background(), testUser{}, testUser{})

	if e.CachedTypes() != 1 {
		t.Fatalf("expected 1 cached type after first diff, got %d", e.CachedTypes())
	}

	// Same type again — count should not grow.
	_, _ = e.Diff(context.Background(), testUser{}, testUser{})
	if e.CachedTypes() != 1 {
		t.Fatalf("expected 1 cached type (no growth), got %d", e.CachedTypes())
	}
}

// ---------------------------------------------------------------------------
// GA-007 — Field Tag Tests
// ---------------------------------------------------------------------------

func TestDiff_IgnoredField(t *testing.T) {
	type entity struct {
		Name     string
		Internal string `version:"ignore"`
		Age      int
	}
	e := diff.New()
	prev := entity{Name: "Alice", Internal: "secret1", Age: 30}
	curr := entity{Name: "Alice", Internal: "secret2", Age: 30}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta (ignored field changed), got %d changes", len(delta.Changes))
	}
}

func TestDiff_TrackedField(t *testing.T) {
	// When any field is "tracked", only tracked fields should be compared.
	type entity struct {
		Name    string `version:"tracked"`
		Email   string `version:"tracked"`
		Comment string // not tracked — should be excluded
	}
	e := diff.New()
	prev := entity{Name: "Alice", Email: "a@b.com", Comment: "old"}
	curr := entity{Name: "Alice", Email: "a@b.com", Comment: "new"}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Comment changed but is not tracked — should produce empty delta.
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta (only untracked field changed), got %d changes", len(delta.Changes))
	}

	// Now change a tracked field.
	curr.Email = "new@b.com"
	delta, err = e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change (tracked Email), got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Email" {
		t.Errorf("expected path %q, got %q", "Email", delta.Changes[0].Path)
	}
}

func TestDiff_IDField(t *testing.T) {
	type entity struct {
		ID   string `version:"id"`
		Name string
		Age  int
	}
	e := diff.New()
	prev := entity{ID: "usr_1", Name: "Alice", Age: 30}
	curr := entity{ID: "usr_2", Name: "Alice", Age: 31}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ID changed but is tagged "id" — must not appear in delta.
	// Only Age should appear.
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change (Age), got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Age" {
		t.Errorf("expected path %q, got %q", "Age", delta.Changes[0].Path)
	}
}

func TestDiff_CommaSeparatedTags(t *testing.T) {
	type entity struct {
		Name  string `version:"tracked,redactable"`
		Email string `version:"tracked"`
		Notes string // not tracked
	}
	e := diff.New()
	prev := entity{Name: "Alice", Email: "a@b.com", Notes: "old"}
	curr := entity{Name: "Bob", Email: "a@b.com", Notes: "new"}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only Name should change (tracked). Notes is untracked.
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Name" {
		t.Errorf("expected path %q, got %q", "Name", delta.Changes[0].Path)
	}
}

// ---------------------------------------------------------------------------
// GA-008 — Nested + Embedded Struct Tests
// ---------------------------------------------------------------------------

func TestDiff_NestedStruct_LeafChange(t *testing.T) {
	type Address struct {
		City  string
		State string
	}
	type Person struct {
		Name    string
		Address Address
	}
	e := diff.New()
	prev := Person{Name: "Alice", Address: Address{City: "NYC", State: "NY"}}
	curr := Person{Name: "Alice", Address: Address{City: "LA", State: "NY"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Address.City" {
		t.Errorf("expected path %q, got %q", "Address.City", delta.Changes[0].Path)
	}
}

func TestDiff_NestedStruct_5Levels(t *testing.T) {
	type L5 struct{ Value string }
	type L4 struct{ L5 L5 }
	type L3 struct{ L4 L4 }
	type L2 struct{ L3 L3 }
	type L1 struct{ L2 L2 }

	e := diff.New()
	prev := L1{L2: L2{L3: L3{L4: L4{L5: L5{Value: "old"}}}}}
	curr := L1{L2: L2{L3: L3{L4: L4{L5: L5{Value: "new"}}}}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	expected := "L2.L3.L4.L5.Value"
	if delta.Changes[0].Path != expected {
		t.Errorf("expected path %q, got %q", expected, delta.Changes[0].Path)
	}
}

func TestDiff_EmbeddedStruct(t *testing.T) {
	type Timestamps struct {
		CreatedBy string
		UpdatedBy string
	}
	type Entity struct {
		Timestamps // embedded — fields should be promoted
		Name       string
	}
	e := diff.New()
	prev := Entity{Timestamps: Timestamps{CreatedBy: "alice", UpdatedBy: "alice"}, Name: "Widget"}
	curr := Entity{Timestamps: Timestamps{CreatedBy: "alice", UpdatedBy: "bob"}, Name: "Widget"}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	// Embedded fields should appear as top-level (promoted), not "Timestamps.UpdatedBy".
	if delta.Changes[0].Path != "UpdatedBy" {
		t.Errorf("expected promoted path %q, got %q", "UpdatedBy", delta.Changes[0].Path)
	}
}

func TestDiff_NestedStruct_PathFormat(t *testing.T) {
	type Notifications struct {
		Email bool
		SMS   bool
	}
	type Settings struct {
		Notifications Notifications
	}
	type User struct {
		Name     string
		Settings Settings
	}
	e := diff.New()
	prev := User{Name: "Alice", Settings: Settings{Notifications: Notifications{Email: true, SMS: false}}}
	curr := User{Name: "Alice", Settings: Settings{Notifications: Notifications{Email: true, SMS: true}}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	expected := "Settings.Notifications.SMS"
	if delta.Changes[0].Path != expected {
		t.Errorf("expected path %q, got %q", expected, delta.Changes[0].Path)
	}
}

// ---------------------------------------------------------------------------
// GA-009 — Map + Nested JSON Tests
// ---------------------------------------------------------------------------

func TestDiff_Map_ValueChanged(t *testing.T) {
	type Entity struct {
		Tags map[string]string
	}
	e := diff.New()
	prev := Entity{Tags: map[string]string{"env": "prod", "team": "platform"}}
	curr := Entity{Tags: map[string]string{"env": "staging", "team": "platform"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Tags.env" {
		t.Errorf("expected path %q, got %q", "Tags.env", delta.Changes[0].Path)
	}
}

func TestDiff_Map_KeyAdded(t *testing.T) {
	type Entity struct {
		Tags map[string]string
	}
	e := diff.New()
	prev := Entity{Tags: map[string]string{"env": "prod"}}
	curr := Entity{Tags: map[string]string{"env": "prod", "region": "us-east-1"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change (key added), got %d", len(delta.Changes))
	}
	ch := delta.Changes[0]
	if ch.Path != "Tags.region" {
		t.Errorf("expected path %q, got %q", "Tags.region", ch.Path)
	}
	if ch.OldValue != nil {
		t.Errorf("expected nil OldValue for added key, got %s", ch.OldValue)
	}
}

func TestDiff_Map_KeyRemoved(t *testing.T) {
	type Entity struct {
		Tags map[string]string
	}
	e := diff.New()
	prev := Entity{Tags: map[string]string{"env": "prod", "region": "us-east-1"}}
	curr := Entity{Tags: map[string]string{"env": "prod"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change (key removed), got %d", len(delta.Changes))
	}
	ch := delta.Changes[0]
	if ch.Path != "Tags.region" {
		t.Errorf("expected path %q, got %q", "Tags.region", ch.Path)
	}
	if ch.NewValue != nil {
		t.Errorf("expected nil NewValue for removed key, got %s", ch.NewValue)
	}
}

func TestDiff_Map_NonDeterministicOrder(t *testing.T) {
	type Entity struct {
		Tags map[string]string
	}
	e := diff.New()
	m := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}
	entity := Entity{Tags: m}

	// Same map on both sides — no changes regardless of iteration order.
	for i := 0; i < 100; i++ {
		delta, err := e.Diff(context.Background(), entity, entity)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if !delta.IsEmpty() {
			t.Fatalf("iteration %d: expected empty delta, got %d changes", i, len(delta.Changes))
		}
	}
}

func TestDiff_NestedJSON_DeepChange(t *testing.T) {
	type Config struct {
		Settings map[string]any
	}
	e := diff.New()
	prev := Config{Settings: map[string]any{
		"notifications": map[string]any{
			"email": true,
			"sms":   false,
		},
	}}
	curr := Config{Settings: map[string]any{
		"notifications": map[string]any{
			"email": true,
			"sms":   true,
		},
	}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Settings.notifications.sms" {
		t.Errorf("expected path %q, got %q", "Settings.notifications.sms", delta.Changes[0].Path)
	}
}

func TestDiff_NestedJSON_MixedTypes(t *testing.T) {
	type Config struct {
		Data map[string]any
	}
	e := diff.New()
	prev := Config{Data: map[string]any{
		"name":   "Alice",
		"age":    30,
		"active": true,
		"tags":   []any{"a", "b"},
	}}
	curr := Config{Data: map[string]any{
		"name":   "Bob",
		"age":    30,
		"active": false,
		"tags":   []any{"a", "b", "c"},
	}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// name, active, tags changed — age unchanged.
	if len(delta.Changes) != 3 {
		t.Fatalf("expected 3 changes, got %d: %+v", len(delta.Changes), delta.Changes)
	}

	paths := make(map[string]bool)
	for _, c := range delta.Changes {
		paths[c.Path] = true
	}
	for _, expected := range []string{"Data.active", "Data.name", "Data.tags"} {
		if !paths[expected] {
			t.Errorf("expected change at path %q, not found", expected)
		}
	}
}

func TestDiff_NestedJSON_AddedNestedKey(t *testing.T) {
	type Config struct {
		Settings map[string]any
	}
	e := diff.New()
	prev := Config{Settings: map[string]any{
		"theme": "dark",
	}}
	curr := Config{Settings: map[string]any{
		"theme": "dark",
		"notifications": map[string]any{
			"email": true,
		},
	}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change (added nested key), got %d: %+v", len(delta.Changes), delta.Changes)
	}
	ch := delta.Changes[0]
	if ch.Path != "Settings.notifications" {
		t.Errorf("expected path %q, got %q", "Settings.notifications", ch.Path)
	}
	if ch.OldValue != nil {
		t.Errorf("expected nil OldValue, got %s", ch.OldValue)
	}
}

func TestDiff_NestedJSON_RemovedNestedKey(t *testing.T) {
	type Config struct {
		Settings map[string]any
	}
	e := diff.New()
	prev := Config{Settings: map[string]any{
		"theme": "dark",
		"notifications": map[string]any{
			"email": true,
		},
	}}
	curr := Config{Settings: map[string]any{
		"theme": "dark",
	}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change (removed nested key), got %d: %+v", len(delta.Changes), delta.Changes)
	}
	ch := delta.Changes[0]
	if ch.Path != "Settings.notifications" {
		t.Errorf("expected path %q, got %q", "Settings.notifications", ch.Path)
	}
	if ch.NewValue != nil {
		t.Errorf("expected nil NewValue, got %s", ch.NewValue)
	}
}

// ---------------------------------------------------------------------------
// GA-010 — Slices, Pointers, Special Types Tests
// ---------------------------------------------------------------------------

func TestDiff_Slice_ElementChanged(t *testing.T) {
	type Entity struct {
		Tags []string
	}
	e := diff.New()
	prev := Entity{Tags: []string{"go", "rust"}}
	curr := Entity{Tags: []string{"go", "python"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Tags" {
		t.Errorf("expected path %q, got %q", "Tags", delta.Changes[0].Path)
	}
}

func TestDiff_Slice_LengthChanged(t *testing.T) {
	type Entity struct {
		Tags []string
	}
	e := diff.New()
	prev := Entity{Tags: []string{"go"}}
	curr := Entity{Tags: []string{"go", "rust", "python"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Tags" {
		t.Errorf("expected path %q, got %q", "Tags", delta.Changes[0].Path)
	}
}

func TestDiff_Pointer_NilToValue(t *testing.T) {
	type Entity struct {
		Name    string
		Deleted *bool
	}
	e := diff.New()
	tr := true
	prev := Entity{Name: "Alice", Deleted: nil}
	curr := Entity{Name: "Alice", Deleted: &tr}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	ch := delta.Changes[0]
	if ch.Path != "Deleted" {
		t.Errorf("expected path %q, got %q", "Deleted", ch.Path)
	}
	if ch.OldValue != nil {
		t.Errorf("expected nil OldValue for nil→value, got %s", ch.OldValue)
	}
	var newVal bool
	if err := json.Unmarshal(ch.NewValue, &newVal); err != nil {
		t.Fatalf("NewValue is not valid JSON: %v", err)
	}
	if !newVal {
		t.Errorf("expected new value true, got %v", newVal)
	}
}

func TestDiff_Pointer_ValueToNil(t *testing.T) {
	type Entity struct {
		Name    string
		Deleted *bool
	}
	e := diff.New()
	tr := true
	prev := Entity{Name: "Alice", Deleted: &tr}
	curr := Entity{Name: "Alice", Deleted: nil}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	ch := delta.Changes[0]
	if ch.Path != "Deleted" {
		t.Errorf("expected path %q, got %q", "Deleted", ch.Path)
	}
	if ch.NewValue != nil {
		t.Errorf("expected nil NewValue for value→nil, got %s", ch.NewValue)
	}
}

func TestDiff_Time_EqualDifferentTimezone(t *testing.T) {
	type Event struct {
		Name      string
		CreatedAt time.Time
	}
	e := diff.New()
	utc := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	est := utc.In(time.FixedZone("EST", -5*60*60))

	prev := Event{Name: "deploy", CreatedAt: utc}
	curr := Event{Name: "deploy", CreatedAt: est}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Same instant, different TZ — should NOT produce a diff.
	if !delta.IsEmpty() {
		t.Fatalf("expected empty delta (same instant, different TZ), got %d changes: %+v",
			len(delta.Changes), delta.Changes)
	}
}

func TestDiff_CustomType_UnderlyingInt(t *testing.T) {
	type Status int
	const (
		Active  Status = 1
		Deleted Status = 2
	)
	type Entity struct {
		Name   string
		Status Status
	}
	e := diff.New()
	prev := Entity{Name: "Widget", Status: Active}
	curr := Entity{Name: "Widget", Status: Deleted}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(delta.Changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(delta.Changes))
	}
	if delta.Changes[0].Path != "Status" {
		t.Errorf("expected path %q, got %q", "Status", delta.Changes[0].Path)
	}
	var oldStatus, newStatus int
	_ = json.Unmarshal(delta.Changes[0].OldValue, &oldStatus)
	_ = json.Unmarshal(delta.Changes[0].NewValue, &newStatus)
	if oldStatus != 1 || newStatus != 2 {
		t.Errorf("expected 1→2, got %d→%d", oldStatus, newStatus)
	}
}

// Benchmarks — baseline for tracking diff performance over time.
// --------------------------------------------------------------------------

// benchUser is a realistic-size struct for benchmarking (20 fields).
type benchUser struct {
	ID             string
	FirstName      string
	LastName       string
	Email          string
	Phone          string
	Address        string
	City           string
	State          string
	Zip            string
	Country        string
	Company        string
	Department     string
	Title          string
	Role           string
	Active         bool
	LoginCount     int
	FailedAttempts int
	Score          float64
	Level          int
	Flags          uint64
}

func BenchmarkDiff_NoChanges_20Fields(b *testing.B) {
	e := diff.New()
	ctx := context.Background()
	u := benchUser{
		ID: "usr_1", FirstName: "Alice", LastName: "Smith", Email: "a@b.com",
		Phone: "555-1234", Address: "123 Main", City: "NYC", State: "NY",
		Zip: "10001", Country: "US", Company: "Acme", Department: "Eng",
		Title: "Sr Eng", Role: "admin", Active: true, LoginCount: 42,
		FailedAttempts: 0, Score: 98.5, Level: 3, Flags: 0xFF,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Diff(ctx, u, u)
	}
}

func BenchmarkDiff_TwoChanges_20Fields(b *testing.B) {
	e := diff.New()
	ctx := context.Background()
	prev := benchUser{
		ID: "usr_1", FirstName: "Alice", LastName: "Smith", Email: "a@b.com",
		Phone: "555-1234", Address: "123 Main", City: "NYC", State: "NY",
		Zip: "10001", Country: "US", Company: "Acme", Department: "Eng",
		Title: "Sr Eng", Role: "admin", Active: true, LoginCount: 42,
		FailedAttempts: 0, Score: 98.5, Level: 3, Flags: 0xFF,
	}
	curr := prev
	curr.Email = "alice@newmail.com"
	curr.LoginCount = 43

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Diff(ctx, prev, curr)
	}
}

// ---------------------------------------------------------------------------
// GA-011: Normalization, Apply, and Graceful Degradation
// ---------------------------------------------------------------------------

// TestNormalize_SortedSlice verifies that version:"normalized" sorts slices
// before comparison so reordering does not produce spurious diffs.
func TestNormalize_SortedSlice(t *testing.T) {
	type Config struct {
		Tags []string `version:"normalized"`
	}

	e := diff.New()

	prev := Config{Tags: []string{"beta", "alpha", "gamma"}}
	curr := Config{Tags: []string{"gamma", "alpha", "beta"}}

	delta, err := e.Diff(context.Background(), prev, curr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delta.IsEmpty() {
		t.Errorf("expected no changes after normalization, got %d change(s): %+v", len(delta.Changes), delta.Changes)
	}

	// But without the tag, reordering IS a change.
	type ConfigNoTag struct {
		Tags []string
	}
	prev2 := ConfigNoTag{Tags: []string{"beta", "alpha", "gamma"}}
	curr2 := ConfigNoTag{Tags: []string{"gamma", "alpha", "beta"}}

	delta2, err := e.Diff(context.Background(), prev2, curr2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if delta2.IsEmpty() {
		t.Error("expected change without normalized tag, got empty delta")
	}
}

// TestApply_ReconstructFromDelta verifies that Apply produces the correct
// current state from a previous state and a delta.
func TestApply_ReconstructFromDelta(t *testing.T) {
	e := diff.New()
	ctx := context.Background()

	prev := testUser{Name: "Alice", Email: "old@test.com", Age: 30}
	curr := testUser{Name: "Alice", Email: "new@test.com", Age: 31}

	delta, err := e.Diff(ctx, prev, curr)
	if err != nil {
		t.Fatalf("Diff error: %v", err)
	}

	result, err := e.Apply(ctx, prev, delta)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	got, ok := result.(testUser)
	if !ok {
		t.Fatalf("Apply returned %T, want testUser", result)
	}
	if got != curr {
		t.Errorf("Apply result = %+v, want %+v", got, curr)
	}
}

// TestApply_RoundTrip verifies Apply(prev, Diff(prev, curr)) == curr for
// 10 different entity shapes covering flat, nested, embedded, pointer,
// time, map, slice, custom type, mixed, and empty-delta cases.
func TestApply_RoundTrip(t *testing.T) {
	type Address struct {
		City  string
		State string
	}
	type Embedded struct {
		Address
		Name string
	}
	type WithPointer struct {
		Name  string
		Alias *string
	}
	type WithTime struct {
		Name      string
		CreatedAt time.Time
	}
	type WithMap struct {
		Name string
		Meta map[string]string
	}
	type WithSlice struct {
		Name string
		Tags []string
	}
	type Status int
	type WithCustom struct {
		Name   string
		Status Status
	}
	type Mixed struct {
		Name   string
		Score  float64
		Active bool
		Count  int
	}

	alias1 := "bob"
	alias2 := "robert"

	cases := []struct {
		name string
		prev any
		curr any
	}{
		{"flat", testUser{Name: "A", Email: "a@b", Age: 1}, testUser{Name: "B", Email: "a@b", Age: 1}},
		{"nested", struct {
			Addr Address
		}{Address{"NYC", "NY"}}, struct {
			Addr Address
		}{Address{"LA", "CA"}}},
		{"embedded", Embedded{Address: Address{"NYC", "NY"}, Name: "X"}, Embedded{Address: Address{"LA", "CA"}, Name: "Y"}},
		{"pointer-nil-to-val", WithPointer{Name: "A", Alias: nil}, WithPointer{Name: "A", Alias: &alias1}},
		{"pointer-val-change", WithPointer{Name: "A", Alias: &alias1}, WithPointer{Name: "A", Alias: &alias2}},
		{"time", WithTime{Name: "A", CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, WithTime{Name: "A", CreatedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}},
		{"map", WithMap{Name: "A", Meta: map[string]string{"k": "v1"}}, WithMap{Name: "A", Meta: map[string]string{"k": "v2"}}},
		{"slice", WithSlice{Name: "A", Tags: []string{"a", "b"}}, WithSlice{Name: "A", Tags: []string{"a", "c"}}},
		{"custom-type", WithCustom{Name: "A", Status: 1}, WithCustom{Name: "A", Status: 2}},
		{"no-changes", Mixed{Name: "A", Score: 1.0, Active: true, Count: 1}, Mixed{Name: "A", Score: 1.0, Active: true, Count: 1}},
	}

	e := diff.New()
	ctx := context.Background()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delta, err := e.Diff(ctx, tc.prev, tc.curr)
			if err != nil {
				t.Fatalf("Diff: %v", err)
			}

			result, err := e.Apply(ctx, tc.prev, delta)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			// Re-diff the result against curr — should produce empty delta.
			check, err := e.Diff(ctx, result, tc.curr)
			if err != nil {
				t.Fatalf("re-Diff: %v", err)
			}
			if !check.IsEmpty() {
				t.Errorf("round-trip mismatch: re-diff produced %d change(s): %+v", len(check.Changes), check.Changes)
			}
		})
	}
}

// TestDiff_UnsupportedType_FallsBack verifies that a struct containing a
// chan field returns ErrDiffFallback (not a panic).
func TestDiff_UnsupportedType_FallsBack(t *testing.T) {
	type WithChan struct {
		Name string
		Ch   chan int
	}

	e := diff.New()
	ch := make(chan int)

	prev := WithChan{Name: "A", Ch: ch}
	curr := WithChan{Name: "B", Ch: ch}

	_, err := e.Diff(context.Background(), prev, curr)
	if err == nil {
		t.Fatal("expected error for chan field, got nil")
	}
	if !errors.Is(err, domain.ErrDiffFallback) {
		t.Errorf("expected ErrDiffFallback, got: %v", err)
	}
}

// TestDiff_PanicRecovery verifies that SafeDiff recovers from a panic during
// diff and returns ErrDiffFallback instead of crashing.
func TestDiff_PanicRecovery(t *testing.T) {
	e := diff.New()

	type panicEntity struct {
		Name string
		Bad  panicOnMarshal
	}

	// "ok"→"BOOM": diff detects a change in Bad, then json.Marshal on the
	// new value triggers MarshalJSON which panics. SafeDiff must recover.
	prev := panicEntity{Name: "A", Bad: "ok"}
	curr := panicEntity{Name: "A", Bad: "BOOM"}

	delta, err := diff.SafeDiff(context.Background(), e, prev, curr)
	if err == nil {
		t.Fatal("expected error from panic recovery, got nil")
	}
	if !errors.Is(err, domain.ErrDiffFallback) {
		t.Errorf("expected ErrDiffFallback, got: %v", err)
	}
	if !delta.IsEmpty() {
		t.Error("expected empty delta on panic recovery")
	}
}

// panicOnMarshal is a named string type that panics during JSON marshalling
// when it contains "BOOM". As a non-struct leaf, the diff engine marshals
// it directly when it detects a change.
type panicOnMarshal string

func (p panicOnMarshal) MarshalJSON() ([]byte, error) {
	if p == "BOOM" {
		panic("boom: intentional panic for testing")
	}
	return json.Marshal(string(p))
}
