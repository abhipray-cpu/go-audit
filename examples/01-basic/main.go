// Example 01-basic demonstrates the minimal go-audit workflow:
// register an entity, create versions, and query them back.
package main

import (
	"context"
	"fmt"
	"log"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	audittest "github.com/abhipray-cpu/go-audit/testing"
)

// User is a simple domain entity with audit tags.
type User struct {
	ID    string `version:"id"`
	Name  string `version:"tracked"`
	Email string `version:"tracked"`
}

func main() {
	// For this example we use the in-memory store.
	// In production, swap for adapter/postgres.Writer and Reader.
	store := audittest.NewInMemoryStore()

	auditor, err := audit.New(audit.Config{
		Writer: store,
		Reader: store,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer auditor.Shutdown(context.Background())

	// Register entity types once at startup.
	if err := auditor.Register(&User{}); err != nil {
		log.Fatal(err)
	}

	// Attach actor metadata to the context.
	ctx := audit.WithActor(context.Background(), "admin-1", domain.ActorHuman)
	ctx = audit.WithReason(ctx, "initial creation")

	// --- Version 1: create the user ---
	user := &User{ID: "user-42", Name: "Alice", Email: "alice@example.com"}
	p1, err := auditor.Version(ctx, user, port.WithSync())
	if err != nil {
		log.Fatal(err)
	}
	r1, err := p1.Wait(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("v%d created (actor=%s, reason=%s)\n",
		r1.Version,
		r1.Metadata.ActorID,
		r1.Metadata.Reason,
	)

	// --- Version 2: update the user ---
	ctx = audit.WithReason(ctx, "name change")
	user.Name = "Bob"
	p2, err := auditor.Version(ctx, user, port.WithSync())
	if err != nil {
		log.Fatal(err)
	}
	r2, err := p2.Wait(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("v%d created\n", r2.Version)

	// --- Query: get latest ---
	latest, err := auditor.GetLatest(ctx, "user", "user-42")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Latest version: %d\n", latest.Version)

	// --- Query: get specific version ---
	v1, err := auditor.GetVersion(ctx, "user", "user-42", 1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Version 1 entity_type=%s\n", v1.EntityType)

	// --- Query: list all versions ---
	versions, err := auditor.ListVersions(ctx, "user", "user-42")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Total versions: %d\n", len(versions))
}
