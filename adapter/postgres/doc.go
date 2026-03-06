// Package postgres implements the PostgreSQL storage adapter for go-audit.
//
// This adapter provides VersionWriterPort and VersionReaderPort implementations
// backed by PostgreSQL. It is distributed as a separate Go sub-module to avoid
// pulling pgx into projects that don't use PostgreSQL.
//
// Import path: github.com/abhipray-cpu/go-audit/adapter/postgres
package postgres
