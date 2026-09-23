// Package platform contains cross-cutting service primitives.
package platform

import "context"

type DependencyHealth struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type HealthChecker interface {
	CheckHealth(ctx context.Context) DependencyHealth
}
