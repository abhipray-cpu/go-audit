// Package db provides a database-backed WAL adapter for go-audit.
//
// DBWALAdapter uses a staging table in the primary database for WAL entries.
// This is the recommended WAL for multi-pod deployments where a shared
// database is available.
package db
