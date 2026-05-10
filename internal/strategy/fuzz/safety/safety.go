// Package safety implements the fuzz-strategy safety model (profiles, method gates, throttle, timeout).
package safety

import (
	"fmt"
	"strings"
	"time"
)

// Profile selects default allowed HTTP methods before allow/deny overrides.
type Profile string

const (
	ProfileSafe        Profile = "safe"
	ProfileAggressive  Profile = "aggressive"
	ProfileDestructive Profile = "destructive"
)

// SafetyConfig is the complete safety configuration for fuzz runs.
type SafetyConfig struct {
	Profile              Profile
	AllowedMethods       []string
	DeniedMethods        []string
	MaxRequestsPerSecond float64
	MaxConcurrency       int
	TimeoutSeconds       float64
}

type enforcerOpts struct {
	destructiveConfirmed bool
}

// Option configures NewEnforcer.
type Option func(*enforcerOpts)

// WithDestructiveConfirmed must be true to use ProfileDestructive (CLI-only opt-in).
func WithDestructiveConfirmed(v bool) Option {
	return func(o *enforcerOpts) {
		o.destructiveConfirmed = v
	}
}

// Enforcer is the runtime gatekeeper for fuzz requests. It is safe for concurrent use.
type Enforcer struct {
	cfg SafetyConfig
}

// NewEnforcer validates cfg and returns an enforcer.
func NewEnforcer(cfg SafetyConfig, opts ...Option) (*Enforcer, error) {
	var o enforcerOpts
	for _, fn := range opts {
		fn(&o)
	}

	p := normalizeProfile(cfg.Profile)
	if p == "" {
		return nil, fmt.Errorf("safety: profile is required")
	}
	switch p {
	case ProfileSafe, ProfileAggressive, ProfileDestructive:
	default:
		return nil, fmt.Errorf("safety: unknown profile %q", p)
	}

	if cfg.MaxRequestsPerSecond < 0 {
		return nil, fmt.Errorf("safety: MaxRequestsPerSecond must be >= 0")
	}
	if cfg.TimeoutSeconds < 0 {
		return nil, fmt.Errorf("safety: TimeoutSeconds must be >= 0")
	}
	if cfg.MaxConcurrency < 0 {
		return nil, fmt.Errorf("safety: MaxConcurrency must be >= 0")
	}

	if p == ProfileDestructive && !o.destructiveConfirmed {
		return nil, fmt.Errorf("safety: destructive profile requires WithDestructiveConfirmed(true)")
	}

	cfg.Profile = p
	return &Enforcer{cfg: cfg}, nil
}

func normalizeProfile(p Profile) Profile {
	return Profile(strings.TrimSpace(string(p)))
}

// Allow reports whether method is permitted under cfg (deny wins, then explicit allow list, then profile defaults).
func (e *Enforcer) Allow(method string) bool {
	m := strings.ToUpper(strings.TrimSpace(method))
	if m == "" {
		return false
	}
	for _, d := range e.cfg.DeniedMethods {
		if strings.ToUpper(strings.TrimSpace(d)) == m {
			return false
		}
	}
	if len(e.cfg.AllowedMethods) > 0 {
		for _, a := range e.cfg.AllowedMethods {
			if strings.ToUpper(strings.TrimSpace(a)) == m {
				return true
			}
		}
		return false
	}
	return e.allowProfileDefault(m)
}

func (e *Enforcer) allowProfileDefault(m string) bool {
	switch e.cfg.Profile {
	case ProfileDestructive:
		return true
	case ProfileAggressive:
		switch m {
		case "GET", "POST", "PATCH", "PUT":
			return true
		default:
			return false
		}
	case ProfileSafe:
		switch m {
		case "GET", "POST", "PATCH":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// ThrottleDelay returns the minimum delay between requests from MaxRequestsPerSecond (0 => no delay).
func (e *Enforcer) ThrottleDelay() time.Duration {
	if e.cfg.MaxRequestsPerSecond <= 0 {
		return 0
	}
	return time.Duration(float64(time.Second) / e.cfg.MaxRequestsPerSecond)
}

// RequestTimeout returns per-request timeout (default 30s when TimeoutSeconds is 0).
func (e *Enforcer) RequestTimeout() time.Duration {
	if e.cfg.TimeoutSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(e.cfg.TimeoutSeconds * float64(time.Second))
}
