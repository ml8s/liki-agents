package buildinfo

import "runtime/debug"

var (
	Version      = "dev"
	Commit       = "unknown"
	BuildTime    = "unknown"
	GraphVersion = "adk-single-expert-v0"
)

func Map() map[string]string {
	info := map[string]string{
		"service":       "liki-agent",
		"version":       Version,
		"commit":        Commit,
		"build_time":    BuildTime,
		"graph_version": GraphVersion,
	}
	if data, ok := debug.ReadBuildInfo(); ok {
		info["go_version"] = data.GoVersion
	}
	return info
}
