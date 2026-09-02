package safety_test

import (
	"testing"
	"time"

	"github.com/jimbery/bt/internal/strategy/fuzz/safety"
)

func TestEnforcer_SafeProfile_AllowsGET(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if !e.Allow("GET") {
		t.Error("safe profile must allow GET")
	}
}

func TestEnforcer_SafeProfile_AllowsPOST(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if !e.Allow("POST") {
		t.Error("safe profile must allow POST")
	}
}

func TestEnforcer_SafeProfile_AllowsPATCH(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if !e.Allow("PATCH") {
		t.Error("safe profile must allow PATCH")
	}
}

func TestEnforcer_SafeProfile_BlocksDELETE(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if e.Allow("DELETE") {
		t.Error("safe profile must block DELETE")
	}
}

func TestEnforcer_SafeProfile_BlocksPUT(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if e.Allow("PUT") {
		t.Error("safe profile must block PUT")
	}
}

func TestEnforcer_SafeProfile_BlocksHEAD(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if e.Allow("HEAD") {
		t.Error("safe profile must block HEAD")
	}
}

func TestEnforcer_AggressiveProfile_AllowsPUT(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileAggressive})
	if !e.Allow("PUT") {
		t.Error("aggressive profile must allow PUT")
	}
}

func TestEnforcer_AggressiveProfile_BlocksDELETE(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileAggressive})
	if e.Allow("DELETE") {
		t.Error("aggressive profile must block DELETE")
	}
}

func TestEnforcer_DestructiveProfile_RequiresExplicitConfirmation(t *testing.T) {
	_, err := safety.NewEnforcer(safety.SafetyConfig{Profile: safety.ProfileDestructive})
	if err == nil {
		t.Error("expected error when building destructive enforcer without explicit confirmation")
	}
}

func TestEnforcer_DestructiveProfile_WithConfirmation_AllowsDELETE(t *testing.T) {
	e, err := safety.NewEnforcer(
		safety.SafetyConfig{Profile: safety.ProfileDestructive},
		safety.WithDestructiveConfirmed(true),
	)
	if err != nil {
		t.Fatalf("unexpected error with destructive confirmation: %v", err)
	}
	if !e.Allow("DELETE") {
		t.Error("destructive profile with confirmation must allow DELETE")
	}
}

func TestEnforcer_AllowedMethods_OverridesProfileDefaults(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:        safety.ProfileSafe,
		AllowedMethods: []string{"GET"},
	})
	if e.Allow("POST") {
		t.Error("POST should be blocked when not in AllowedMethods override")
	}
	if !e.Allow("GET") {
		t.Error("GET should be allowed when in AllowedMethods override")
	}
}

func TestEnforcer_DeniedMethods_WinsOverAllowedMethods(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:        safety.ProfileSafe,
		AllowedMethods: []string{"GET", "POST"},
		DeniedMethods:  []string{"POST"},
	})
	if e.Allow("POST") {
		t.Error("POST must be blocked when it appears in DeniedMethods, even if in AllowedMethods")
	}
	if !e.Allow("GET") {
		t.Error("GET must be allowed when in AllowedMethods and not in DeniedMethods")
	}
}

func TestEnforcer_DeniedMethods_WinsOverProfileDefaults(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:       safety.ProfileSafe,
		DeniedMethods: []string{"GET"},
	})
	if e.Allow("GET") {
		t.Error("GET must be blocked when explicitly in DeniedMethods")
	}
}

func TestEnforcer_MethodCheck_IsCaseInsensitive(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if !e.Allow("get") {
		t.Error("Allow should be case-insensitive: 'get' should be treated as 'GET'")
	}
	if e.Allow("delete") {
		t.Error("Allow should be case-insensitive: 'delete' should be treated as 'DELETE' and blocked")
	}
}

func TestEnforcer_ThrottleDelay_ZeroRPS_ReturnsZero(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:              safety.ProfileSafe,
		MaxRequestsPerSecond: 0,
	})
	if d := e.ThrottleDelay(); d != 0 {
		t.Errorf("expected zero delay when MaxRequestsPerSecond is 0, got %v", d)
	}
}

func TestEnforcer_ThrottleDelay_10RPS_Returns100ms(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:              safety.ProfileSafe,
		MaxRequestsPerSecond: 10,
	})
	expected := 100 * time.Millisecond
	if d := e.ThrottleDelay(); d != expected {
		t.Errorf("expected %v for 10 RPS, got %v", expected, d)
	}
}

func TestEnforcer_ThrottleDelay_1RPS_Returns1Second(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:              safety.ProfileSafe,
		MaxRequestsPerSecond: 1,
	})
	expected := 1 * time.Second
	if d := e.ThrottleDelay(); d != expected {
		t.Errorf("expected %v for 1 RPS, got %v", expected, d)
	}
}

func TestEnforcer_RequestTimeout_Default_Is30Seconds(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	expected := 30 * time.Second
	if to := e.RequestTimeout(); to != expected {
		t.Errorf("expected default timeout %v, got %v", expected, to)
	}
}

func TestEnforcer_RequestTimeout_CustomValue_IsRespected(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:        safety.ProfileSafe,
		TimeoutSeconds: 5,
	})
	expected := 5 * time.Second
	if to := e.RequestTimeout(); to != expected {
		t.Errorf("expected %v, got %v", expected, to)
	}
}

func TestNewEnforcer_NegativeRPS_ReturnsError(t *testing.T) {
	_, err := safety.NewEnforcer(safety.SafetyConfig{
		Profile:              safety.ProfileSafe,
		MaxRequestsPerSecond: -1,
	})
	if err == nil {
		t.Error("expected error for negative MaxRequestsPerSecond")
	}
}

func TestNewEnforcer_NegativeTimeout_ReturnsError(t *testing.T) {
	_, err := safety.NewEnforcer(safety.SafetyConfig{
		Profile:        safety.ProfileSafe,
		TimeoutSeconds: -5,
	})
	if err == nil {
		t.Error("expected error for negative TimeoutSeconds")
	}
}

func TestNewEnforcer_UnknownProfile_ReturnsError(t *testing.T) {
	_, err := safety.NewEnforcer(safety.SafetyConfig{Profile: "yolo"})
	if err == nil {
		t.Error("expected error for unknown profile")
	}
}

func TestNewEnforcer_EmptyProfile_ReturnsError(t *testing.T) {
	_, err := safety.NewEnforcer(safety.SafetyConfig{})
	if err == nil {
		t.Error("expected error for empty profile")
	}
}

func TestEnforcer_Allow_IsSafeForConcurrentUse(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func() {
			e.Allow("GET")
			e.Allow("DELETE")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
}

func mustEnforcer(t *testing.T, cfg safety.SafetyConfig, opts ...safety.Option) *safety.Enforcer {
	t.Helper()
	e, err := safety.NewEnforcer(cfg, opts...)
	if err != nil {
		t.Fatalf("unexpected error building enforcer: %v", err)
	}
	return e
}
