// Example 02-transaction demonstrates VersionInTx — creating an audit
// version within a caller-managed database transaction.
//
// In production you would use a real *sql.Tx or pgx.Tx. This example
// uses the in-memory store which ignores the transaction handle, but
// the API shape is identical.
package main

import (
	"context"
	"fmt"
	"log"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	audittest "github.com/abhipray-cpu/go-audit/testing"
)

type Order struct {
	ID     string  `version:"id"`
	Status string  `version:"tracked"`
	Total  float64 `version:"tracked"`
}

func main() {
	store := audittest.NewInMemoryStore()

	auditor, err := audit.New(audit.Config{
		Writer: store,
		Reader: store,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer auditor.Shutdown(context.Background())

	if err := auditor.Register(&Order{}); err != nil {
		log.Fatal(err)
	}

	ctx := audit.WithActor(context.Background(), "payment-svc", domain.ActorService)
	ctx = audit.WithReason(ctx, "payment confirmed")

	// Simulate a transaction handle (in production: tx, _ := db.BeginTx(ctx, nil))
	fakeTx := "simulated-tx"

	order := &Order{ID: "order-99", Status: "confirmed", Total: 149.99}
	result, err := auditor.VersionInTx(ctx, fakeTx, order)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("v%d created in transaction (actor=%s)\n",
		result.Record.Version,
		result.Record.Metadata.ActorID,
	)
	fmt.Printf("Entity: %s/%s\n",
		result.Record.EntityType,
		result.Record.EntityID,
	)
}
