//go:build integration

package integration

import "time"

// ---------------------------------------------------------------------------
// Entity catalog — every struct exercised by the integration tests.
//
// These are defined centrally so all test files share the same types.
// Each struct is designed to exercise a specific diff-engine code path.
// ---------------------------------------------------------------------------

// FlatEntity exercises primitive field diffing: string, int, float64, bool.
type FlatEntity struct {
	ID     string  `version:"id"`
	Name   string  `version:"tracked"`
	Age    int     `version:"tracked"`
	Score  float64 `version:"tracked"`
	Active bool    `version:"tracked"`
}

// Address is a value-type nested struct used inside NestedEntity and PointerEntity.
type Address struct {
	Street  string
	City    string
	Country string
	ZipCode string
}

// Contact is a second nested struct for cross-nest diff tests.
type Contact struct {
	Email string
	Phone string
}

// NestedEntity exercises 2-level struct nesting.
type NestedEntity struct {
	ID      string  `version:"id"`
	Name    string  `version:"tracked"`
	Address Address `version:"tracked"`
	Contact Contact `version:"tracked"`
}

// GeoCoord is a leaf struct at depth 4.
type GeoCoord struct {
	Lat float64
	Lng float64
}

// Location wraps a GeoCoord with a label.
type Location struct {
	Label string
	Coord GeoCoord
}

// Office wraps a Location with a name.
type Office struct {
	Name     string
	Location Location
}

// Company exercises 4-level deep nesting (Company → Office → Location → GeoCoord).
type Company struct {
	ID           string `version:"id"`
	Name         string `version:"tracked"`
	Headquarters Office `version:"tracked"`
}

// MapEntity exercises string-keyed map diffing including nested maps and map[string]any.
type MapEntity struct {
	ID       string                       `version:"id"`
	Tags     map[string]string            `version:"tracked"`
	Settings map[string]any               `version:"tracked"`
	Nested   map[string]map[string]string `version:"tracked"`
}

// SliceEntity exercises slice diffing with both ordered and normalized comparison.
type SliceEntity struct {
	ID         string   `version:"id"`
	Roles      []string `version:"tracked"`
	Scores     []int    `version:"tracked"`
	SortedTags []string `version:"tracked,normalized"`
}

// PointerEntity exercises pointer fields and nil ↔ value transitions.
type PointerEntity struct {
	ID       string   `version:"id"`
	Nickname *string  `version:"tracked"`
	Address  *Address `version:"tracked"`
	Score    *int     `version:"tracked"`
}

// TimeEntity exercises time.Time diffing including timezone-independence.
type TimeEntity struct {
	ID        string    `version:"id"`
	CreatedAt time.Time `version:"tracked"`
	UpdatedAt time.Time `version:"tracked"`
}

// AuditFields is embedded (promoted) in EmbeddedEntity.
type AuditFields struct {
	CreatedBy string
	UpdatedBy string
}

// EmbeddedEntity exercises anonymous (promoted) struct field diffing.
type EmbeddedEntity struct {
	ID   string `version:"id"`
	Name string `version:"tracked"`
	AuditFields
}

// TaggedEntity exercises every version-tag combination.
type TaggedEntity struct {
	ID         string `version:"id"`
	Tracked1   string `version:"tracked"`
	Tracked2   int    `version:"tracked"`
	Ignored    string `version:"ignore"`
	Redactable string `version:"tracked,redactable"`
	Normal     string // no tag — excluded in tracked mode
}

// FallbackChanEntity has a chan field that should cause ErrDiffFallback.
type FallbackChanEntity struct {
	ID   string   `version:"id"`
	Ch   chan int `version:"tracked"`
	Name string   `version:"tracked"`
}

// FallbackFuncEntity has a func field that should cause ErrDiffFallback.
type FallbackFuncEntity struct {
	ID   string `version:"id"`
	Fn   func() `version:"tracked"`
	Name string `version:"tracked"`
}

// UserEntity is a realistic domain model used in Phase 2 end-to-end tests.
type UserEntity struct {
	ID        string            `version:"id"`
	Name      string            `version:"tracked"`
	Email     string            `version:"tracked,redactable"`
	Age       int               `version:"tracked"`
	Address   Address           `version:"tracked"`
	Roles     []string          `version:"tracked"`
	Settings  map[string]string `version:"tracked"`
	CreatedAt time.Time         `version:"tracked"`
}

// OrderItem is a line item inside an order.
type OrderItem struct {
	SKU      string
	Quantity int
	Price    float64
}

// OrderEntity is a multi-field domain model for Phase 2.
type OrderEntity struct {
	ID     string      `version:"id"`
	UserID string      `version:"tracked"`
	Items  []OrderItem `version:"tracked"`
	Total  float64     `version:"tracked"`
	Status string      `version:"tracked"`
}

// ProductEntity is a simple domain model for Phase 2 multi-entity tests.
type ProductEntity struct {
	ID    string            `version:"id"`
	SKU   string            `version:"tracked"`
	Name  string            `version:"tracked"`
	Price float64           `version:"tracked"`
	Tags  map[string]string `version:"tracked"`
}

// Helper constructors.

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
