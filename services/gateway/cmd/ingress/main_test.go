package main

import "testing"

func TestDailyEnvironmentBoundary(t *testing.T) {
	for _, environment := range []string{"development", "production", "staging", ""} {
		for _, setting := range []string{"", "true", "false", "TRUE", "invalid"} {
			got, err := dailyEnabled(environment, setting)
			valid := setting == "" || setting == "true" || setting == "false"
			if (err == nil) != valid {
				t.Fatalf("setting %q validation: %v", setting, err)
			}
			if valid && got != (environment != "development" && setting != "false") {
				t.Fatalf("environment %q, setting %q: %v", environment, setting, got)
			}
		}
	}
}
