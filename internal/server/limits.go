package server

import (
	"fmt"
	"io"
	"sync"

	"github.com/charmbracelet/ssh"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
)

// SessionLimits tracks global and per-player concurrent sessions.
type SessionLimits struct {
	mu        sync.Mutex
	global    int
	perKey    map[string]int
	maxGlobal int
	maxPerKey int
	resolver  *identity.Resolver
}

// NewSessionLimits creates a limiter keyed on the resolved player fingerprint.
func NewSessionLimits(maxGlobal, maxPerKey int, resolver *identity.Resolver) *SessionLimits {
	return &SessionLimits{
		perKey:    make(map[string]int),
		maxGlobal: maxGlobal,
		maxPerKey: maxPerKey,
		resolver:  resolver,
	}
}

// Middleware rejects connections when caps are exceeded.
func (l *SessionLimits) Middleware() func(ssh.Handler) ssh.Handler {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			resolved, err := l.resolver.Resolve(s)
			if err != nil {
				if identity.IsProxiedIdentityError(err) {
					_, _ = io.WriteString(s, identity.ProxiedIdentityMessage())
				} else {
					_, _ = io.WriteString(s, "Public key authentication is required.\r\n")
				}
				s.Exit(1)
				return
			}
			fp := resolved.Identity.Fingerprint
			if !l.acquire(fp) {
				_, _ = io.WriteString(s, fmt.Sprintf(
					"Too many active sessions (global max %d, per-key max %d). Try again later.\r\n",
					l.maxGlobal, l.maxPerKey,
				))
				s.Exit(1)
				return
			}
			defer l.release(fp)
			next(s)
		}
	}
}

func (l *SessionLimits) acquire(fingerprint string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.global >= l.maxGlobal {
		return false
	}
	if l.perKey[fingerprint] >= l.maxPerKey {
		return false
	}

	l.global++
	l.perKey[fingerprint]++
	return true
}

func (l *SessionLimits) release(fingerprint string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.global--
	if l.perKey[fingerprint] > 0 {
		l.perKey[fingerprint]--
		if l.perKey[fingerprint] == 0 {
			delete(l.perKey, fingerprint)
		}
	}
}
