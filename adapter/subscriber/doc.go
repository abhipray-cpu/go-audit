// Package subscriber provides the subscriber fan-out and default subscribers
// for go-audit.
//
// When a version is created, registered subscribers are notified asynchronously
// with failure isolation — one subscriber's failure does not block others.
// The default LogSubscriber emits a structured log entry for each version event.
package subscriber
