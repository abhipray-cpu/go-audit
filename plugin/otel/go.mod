module github.com/abhipray-cpu/go-audit/plugin/otel

go 1.21

require (
	github.com/abhipray-cpu/go-audit v0.0.0
	go.opentelemetry.io/otel v1.24.0
	go.opentelemetry.io/otel/metric v1.24.0
	go.opentelemetry.io/otel/trace v1.24.0
)

require (
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
)

replace github.com/abhipray-cpu/go-audit => ../..
