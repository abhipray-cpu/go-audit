// Package clickhouse implements the ClickHouse OLAP adapter for go-audit.
//
// This adapter provides the OLAPPort implementation with batch inserts,
// async flush buffers, and retry logic. Optimized for analytical queries
// over large version histories.
//
// Import path: github.com/abhipray-cpu/go-audit/adapter/clickhouse
package clickhouse
