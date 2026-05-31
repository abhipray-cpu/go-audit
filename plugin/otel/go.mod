module github.com/abhipray-cpu/go-audit/plugin/otel

go 1.25.0

require (
	github.com/abhipray-cpu/go-audit v0.0.0
	go.opentelemetry.io/otel v1.42.0
	go.opentelemetry.io/otel/metric v1.42.0
	go.opentelemetry.io/otel/trace v1.42.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
)

replace github.com/abhipray-cpu/go-audit => ../..
