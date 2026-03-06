package audit_test

import (
	"context"
	"fmt"
	"time"

	audit "github.com/abhipray-cpu/go-audit"
	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
	audittest "github.com/abhipray-cpu/go-audit/testing"
)

// exUser is a minimal entity for examples.
type exUser struct {
	ID    string `version:"id"`
	Name  string `version:"tracked"`
	Email string `version:"tracked"`
}

func ExampleAuditor_Version() {
	store := audittest.NewInMemoryStore()
	auditor, _ := audit.New(audit.Config{Writer: store, Reader: store})
	defer auditor.Shutdown(context.Background())
	auditor.Register(&exUser{})

	user := &exUser{ID: "user-1", Name: "Alice", Email: "alice@example.com"}
	pending, _ := auditor.Version(context.Background(), user, port.WithSync())
	record, _ := pending.Wait(context.Background())

	fmt.Println(record.Version)
	// Output: 1
}

func ExampleAuditor_GetLatest() {
	store := audittest.NewInMemoryStore()
	auditor, _ := audit.New(audit.Config{Writer: store, Reader: store})
	defer auditor.Shutdown(context.Background())
	auditor.Register(&exUser{})

	user := &exUser{ID: "user-1", Name: "Alice", Email: "alice@example.com"}
	p, _ := auditor.Version(context.Background(), user, port.WithSync())
	p.Wait(context.Background())

	user.Name = "Bob"
	p2, _ := auditor.Version(context.Background(), user, port.WithSync())
	p2.Wait(context.Background())

	latest, _ := auditor.GetLatest(context.Background(), "exuser", "user-1")
	fmt.Println(latest.Version)
	// Output: 2
}

func ExampleAuditor_GetAtTime() {
	store := audittest.NewInMemoryStore()
	auditor, _ := audit.New(audit.Config{Writer: store, Reader: store})
	defer auditor.Shutdown(context.Background())
	auditor.Register(&exUser{})

	user := &exUser{ID: "user-1", Name: "Alice", Email: "alice@example.com"}
	p, _ := auditor.Version(context.Background(), user, port.WithSync())
	p.Wait(context.Background())

	rec, _ := auditor.GetAtTime(context.Background(), "exuser", "user-1", time.Now().Add(time.Hour))
	fmt.Println(rec.EntityID)
	// Output: user-1
}

func ExampleAuditor_Compare() {
	store := audittest.NewInMemoryStore()
	auditor, _ := audit.New(audit.Config{Writer: store, Reader: store})
	defer auditor.Shutdown(context.Background())
	auditor.Register(&exUser{})

	user := &exUser{ID: "user-1", Name: "Alice", Email: "alice@example.com"}
	p1, _ := auditor.Version(context.Background(), user, port.WithSync())
	p1.Wait(context.Background())

	user.Name = "Bob"
	p2, _ := auditor.Version(context.Background(), user, port.WithSync())
	p2.Wait(context.Background())

	delta, _ := auditor.Compare(context.Background(), "exuser", "user-1", 1, 2)
	fmt.Println(len(delta.Changes) > 0)
	// Output: true
}

func ExampleAuditor_VersionInTx() {
	store := audittest.NewInMemoryStore()
	auditor, _ := audit.New(audit.Config{Writer: store, Reader: store})
	defer auditor.Shutdown(context.Background())
	auditor.Register(&exUser{})

	user := &exUser{ID: "user-1", Name: "Alice", Email: "alice@example.com"}
	result, _ := auditor.VersionInTx(context.Background(), "fake-tx", user)

	fmt.Println(result.Record.Version)
	// Output: 1
}

func ExampleWithActor() {
	ctx := context.Background()
	ctx = audit.WithActor(ctx, "user-123", domain.ActorHuman)
	ctx = audit.WithReason(ctx, "profile update")

	meta := audit.MetadataFromCtx(ctx)
	fmt.Println(meta.ActorID)
	fmt.Println(meta.Reason)
	// Output:
	// user-123
	// profile update
}

func ExampleAuditor_VerifyIntegrity() {
	store := audittest.NewInMemoryStore()
	auditor, _ := audit.New(audit.Config{
		Writer:          store,
		Reader:          store,
		EnableHashChain: true,
	})
	defer auditor.Shutdown(context.Background())
	auditor.Register(&exUser{})

	user := &exUser{ID: "user-1", Name: "Alice", Email: "alice@example.com"}
	p, _ := auditor.Version(context.Background(), user, port.WithSync())
	p.Wait(context.Background())

	err := auditor.VerifyIntegrity(context.Background(), "exuser", "user-1")
	fmt.Println(err)
	// Output: <nil>
}
