// Example 03-testing demonstrates using audittest.NewAuditor and assertion
// helpers in unit tests.
package main_test

import (
	"context"
	"testing"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	audittest "github.com/abhipray-cpu/go-audit/testing"
)

type Product struct {
	ID    string  `version:"id"`
	Name  string  `version:"tracked"`
	Price float64 `version:"tracked"`
}

func TestProductVersioning(t *testing.T) {
	// NewAuditor returns a fully-wired auditor backed by an in-memory store.
	auditor, store := audittest.NewAuditor(t)

	// Register the entity type.
	if err := auditor.Register(&Product{}); err != nil {
		t.Fatal(err)
	}

	ctx := audit.WithActor(context.Background(), "catalog-svc", domain.ActorService)

	// Create version 1.
	product := &Product{ID: "prod-1", Name: "Widget", Price: 9.99}
	p1, err := auditor.Version(ctx, product, port.WithSync())
	if err != nil {
		t.Fatal(err)
	}
	r1, err := p1.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Assertions on v1.
	audittest.AssertActorIs(t, r1, "catalog-svc")
	audittest.AssertLatestVersion(t, store, "product", "prod-1", 1)

	// Create version 2 with a price change.
	product.Price = 12.99
	p2, err := auditor.Version(ctx, product, port.WithSync())
	if err != nil {
		t.Fatal(err)
	}
	p2.Wait(ctx)

	// Verify total version count.
	audittest.AssertVersionCount(t, store, "product", "prod-1", 2)
	audittest.AssertLatestVersion(t, store, "product", "prod-1", 2)
}

func TestSeedFixtures(t *testing.T) {
	auditor, store := audittest.NewAuditor(t)

	// SeedUserHistory creates N versions of the FixtureUser entity.
	results := audittest.SeedUserHistory(t, auditor, "u1", 5)

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	audittest.AssertVersionCount(t, store, "fixtureuser", "u1", 5)
	audittest.AssertLatestVersion(t, store, "fixtureuser", "u1", 5)
}
