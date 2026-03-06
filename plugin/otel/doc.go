// Package otel provides the OpenTelemetry instrumentation plugin for go-audit.
//
// This plugin implements the InstrumenterPort with OpenTelemetry spans and
// metrics. It is distributed as a separate Go sub-module to avoid pulling
// OTEL dependencies into projects that don't need observability.
//
// Import path: github.com/abhipray-cpu/go-audit/plugin/otel
package otel
