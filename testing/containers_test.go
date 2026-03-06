package audittest

import (
	"testing"
)

func TestNewTestInfra_StartsContainers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This will skip if Docker is not available.
	infra := NewTestInfra(t)

	if infra.PostgresDSN() == "" {
		t.Error("PostgresDSN() is empty")
	}
}

func TestFormatPostgresDSN(t *testing.T) {
	got := FormatPostgresDSN("localhost", 5432, "audit", "secret", "audit_test")
	want := "postgres://audit:secret@localhost:5432/audit_test?sslmode=disable"
	if got != want {
		t.Errorf("FormatPostgresDSN() = %q, want %q", got, want)
	}
}

func TestIsDockerAvailable(t *testing.T) {
	// Just verify it doesn't panic. The result depends on the host environment.
	_ = isDockerAvailable()
}
