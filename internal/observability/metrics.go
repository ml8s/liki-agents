// Package observability defines protocol observation contracts.
package observability

import "time"

type ProtocolMetrics interface {
	ObserveHTTPRequest(protocol, operation, status string, duration time.Duration)
	StreamOpened(protocol string)
	StreamClosed(protocol string)
}

type DependencyMetrics interface {
	SetDependencyReady(name string, ready bool)
}
