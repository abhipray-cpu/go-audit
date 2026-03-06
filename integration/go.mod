module github.com/abhipray-cpu/go-audit/integration

go 1.24.1

replace (
	github.com/abhipray-cpu/go-audit => ..
	github.com/abhipray-cpu/go-audit/adapter/clickhouse => ../adapter/clickhouse
	github.com/abhipray-cpu/go-audit/adapter/coldstore/s3 => ../adapter/coldstore/s3
	github.com/abhipray-cpu/go-audit/adapter/mysql => ../adapter/mysql
	github.com/abhipray-cpu/go-audit/adapter/postgres => ../adapter/postgres
)

require (
	github.com/ClickHouse/clickhouse-go/v2 v2.43.0
	github.com/abhipray-cpu/go-audit v0.0.0
	github.com/abhipray-cpu/go-audit/adapter/clickhouse v0.0.0-00010101000000-000000000000
	github.com/abhipray-cpu/go-audit/adapter/coldstore/s3 v0.0.0-00010101000000-000000000000
	github.com/abhipray-cpu/go-audit/adapter/mysql v0.0.0-00010101000000-000000000000
	github.com/abhipray-cpu/go-audit/adapter/postgres v0.0.0-00010101000000-000000000000
	github.com/go-sql-driver/mysql v1.9.3
	github.com/jackc/pgx/v5 v5.8.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/ClickHouse/ch-go v0.71.0 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/paulmach/orb v0.12.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	go.opentelemetry.io/otel v1.39.0 // indirect
	go.opentelemetry.io/otel/trace v1.39.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.29.0 // indirect
)
