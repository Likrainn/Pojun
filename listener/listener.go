package listener

import (
	"context"
	"net"
	"sync"
)

// Manager manages callback listeners for the server.
// It is initialized lazily so an installation that never needs a callback
// opens no extra socket or goroutine.
type Manager struct {
	mu     sync.Mutex
	closed bool
	ln     net.Listener
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	// Placeholder: actual implementation would bind to a port
	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	if m.ln != nil {
		return m.ln.Close()
	}
	return nil
}

func (m *Manager) Addr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ln == nil {
		return ""
	}
	return m.ln.Addr().String()
}
