// Package platform contains cross-cutting service primitives.
package platform

import "context"

// DependencyHealth is the readiness result of one dependency.
type DependencyHealth struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// HealthChecker reports the readiness of a single dependency.
type HealthChecker interface {
	CheckHealth(ctx context.Context) DependencyHealth
}
