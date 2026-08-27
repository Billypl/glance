package glance

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func parseLiveUpdatesConfig(t *testing.T, yamlStr string) liveUpdatesField {
	t.Helper()
	var c struct {
		Server struct {
			LiveUpdates liveUpdatesField `yaml:"live-updates"`
		} `yaml:"server"`
	}
	if err := yaml.Unmarshal([]byte(yamlStr), &c); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return c.Server.LiveUpdates
}

func TestLiveUpdatesShorthandTrue(t *testing.T) {
	f := parseLiveUpdatesConfig(t, "server:\n  live-updates: true\n")
	if !f.Enabled {
		t.Error("expected Enabled=true")
	}
	if time.Duration(f.TickInterval) != defaultLiveUpdateTickInterval {
		t.Errorf("expected default tick interval, got %v", time.Duration(f.TickInterval))
	}
	if f.PauseWhenIdle == nil || !*f.PauseWhenIdle {
		t.Error("expected PauseWhenIdle=true by default")
	}
}

func TestLiveUpdatesShorthandFalse(t *testing.T) {
	f := parseLiveUpdatesConfig(t, "server:\n  live-updates: false\n")
	if f.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestLiveUpdatesObjectForm(t *testing.T) {
	input := `
server:
  live-updates:
    enabled: true
    tick-interval: 5s
    ping-interval: 60s
    client-debounce-ms: 200
`
	f := parseLiveUpdatesConfig(t, input)
	if !f.Enabled {
		t.Error("expected Enabled=true")
	}
	if time.Duration(f.TickInterval) != 5*time.Second {
		t.Errorf("expected 5s tick interval, got %v", time.Duration(f.TickInterval))
	}
	if f.ClientDebounceMs != 200 {
		t.Errorf("expected 200ms debounce, got %d", f.ClientDebounceMs)
	}
}

// durationField does not support milliseconds — tick-interval: 500ms must fail at parse time.
func TestLiveUpdatesTickIntervalMillisecondsUnsupported(t *testing.T) {
	var c struct {
		Server struct {
			LiveUpdates liveUpdatesField `yaml:"live-updates"`
		} `yaml:"server"`
	}
	err := yaml.Unmarshal([]byte("server:\n  live-updates:\n    enabled: true\n    tick-interval: 500ms\n"), &c)
	if err == nil {
		t.Error("expected parse error for tick-interval: 500ms (ms not supported by durationField)")
	}
}

func TestLiveUpdatesNegativeDebounce(t *testing.T) {
	var f liveUpdatesField
	err := yaml.Unmarshal([]byte("enabled: true\nclient-debounce-ms: -1\n"), &f)
	if err == nil {
		t.Error("expected error for negative client-debounce-ms")
	}
}

func TestLiveUpdatesMissingSection(t *testing.T) {
	f := parseLiveUpdatesConfig(t, "server:\n  port: 8080\n")
	if f.Enabled {
		t.Error("expected Enabled=false when section absent")
	}
}
