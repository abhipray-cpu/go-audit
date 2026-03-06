module github.com/abhipray-cpu/go-audit/testing/chaos

go 1.24.1

replace (
	github.com/abhipray-cpu/go-audit => ../..
	github.com/abhipray-cpu/go-audit/adapter/coldstore/s3 => ../../adapter/coldstore/s3
	github.com/abhipray-cpu/go-audit/adapter/mysql => ../../adapter/mysql
	github.com/abhipray-cpu/go-audit/adapter/postgres => ../../adapter/postgres
)

require (
	github.com/abhipray-cpu/go-audit v0.0.0
	github.com/abhipray-cpu/go-audit/adapter/coldstore/s3 v0.0.0-00010101000000-000000000000
	github.com/abhipray-cpu/go-audit/adapter/mysql v0.0.0-00010101000000-000000000000
	github.com/abhipray-cpu/go-audit/adapter/postgres v0.0.0-00010101000000-000000000000
	github.com/go-sql-driver/mysql v1.9.3
	github.com/jackc/pgx/v5 v5.8.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	golang.org/x/text v0.29.0 // indirect
)
