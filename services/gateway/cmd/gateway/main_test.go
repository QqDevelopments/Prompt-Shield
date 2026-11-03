package main

import (
	"strconv"
	"testing"
)

func TestPublicLimitSwitchCombinations(t *testing.T) {
	for _, development := range []bool{true, false} {
		for _, daily := range []bool{true, false} {
			for _, ip := range []bool{true, false} {
				limit, disabled, err := publicLimitSettings(development, func(name string) string {
					if name == "PUBLIC_DAILY_IP_LIMIT_ENABLED" {
						return strconv.FormatBool(daily)
					}
					return strconv.FormatBool(ip)
				})
				want := 0
				if !development && daily {
					want = 5
				}
				if err != nil || limit != want || disabled == ip {
					t.Fatalf("dev=%v daily=%v ip=%v: %d %v %v", development, daily, ip, limit, disabled, err)
				}
			}
		}
	}
	for _, name := range []string{"PUBLIC_DAILY_IP_LIMIT_ENABLED", "PUBLIC_IP_RATE_LIMIT_ENABLED"} {
		if _, _, err := publicLimitSettings(false, func(key string) string {
			if key == name {
				return "1"
			}
			return ""
		}); err == nil {
			t.Fatal("invalid switch accepted")
		}
	}
}

func TestPublicDailyLimitEnvironmentBoundary(t *testing.T) {
	if got := publicDailyLimit(true); got != 0 {
		t.Fatalf("development daily limit = %d", got)
	}
	if got := publicDailyLimit(false); got != 5 {
		t.Fatalf("server daily limit = %d", got)
	}
}

func TestPublicOriginValidation(t *testing.T) {
	if !validOrigin("http://127.0.0.1:5173", true) {
		t.Fatal("development loopback origin was rejected")
	}
	for _, value := range []string{"http://example.test", "https://example.test/path", "https://user@example.test"} {
		if validOrigin(value, false) {
			t.Fatalf("invalid production origin %q was accepted", value)
		}
	}
	if !validOrigin("https://example.test", false) {
		t.Fatal("valid production origin was rejected")
	}
}
