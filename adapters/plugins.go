package adapters

import (
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
)

// Plugin packages are deployment registrations; KB settings only refer to a registered ID.
// Registration is pure configuration, and each invocation remains a bounded subprocess.
type PluginSpec struct {
	Kind     string   `json:"kind"`
	Version  string   `json:"version"`
	Protocol string   `json:"protocol"`
	Command  []string `json:"command"`
}

func ValidatePlugins(all map[string]PluginSpec) error {
	for id, p := range all {
		if !core.ValidKB(id) || p.Version == "" || len(p.Command) == 0 || strings.TrimSpace(p.Command[0]) == "" {
			return core.Err("INVALID_ARGUMENT", "invalid registered plugin identity or command", 400)
		}
		if p.Kind == "parser" && p.Protocol == "xrag.parser/v1" {
			continue
		}
		if p.Kind == "extractor" && p.Protocol == "xrag.extract/v1" {
			continue
		}
		return core.Err("INVALID_ARGUMENT", "plugin kind/protocol combination unsupported", 400)
	}
	return nil
}
