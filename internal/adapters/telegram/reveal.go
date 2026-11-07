package telegram

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/crypto"
)

type revealEntry struct {
	encryptedCredential string
	expiresAt           time.Time
	consumed            bool
}

// RevealManager manages single-use, time-limited credential reveals.
type RevealManager struct {
	mu      sync.RWMutex
	entries map[string]*revealEntry
	kek     string
	clock   func() time.Time
}

var (
	globalRevealMu       sync.RWMutex
	globalRevealManagers []*RevealManager
)

// NewRevealManager creates and registers a new RevealManager with an optional KEK.
func NewRevealManager(kek string) *RevealManager {
	mgr := &RevealManager{
		entries: make(map[string]*revealEntry),
		kek:     kek,
		clock:   time.Now,
	}

	globalRevealMu.Lock()
	globalRevealManagers = append(globalRevealManagers, mgr)
	globalRevealMu.Unlock()

	return mgr
}

func (m *RevealManager) now() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

// SetClock configures a custom time provider (e.g. for testing TTL expiry).
func (m *RevealManager) SetClock(clock func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clock = clock
}

// Store encrypts the credential under the manager's KEK and stores it with the given TTL.
// Returns the single-use reveal token.
func (m *RevealManager) Store(credential string, ttl time.Duration) (string, error) {
	if credential == "" {
		return "", nil
	}

	enc, err := crypto.Encrypt(credential, m.kek)
	if err != nil {
		return "", fmt.Errorf("reveal: encrypt credential: %w", err)
	}

	tokenBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
		return "", fmt.Errorf("reveal: generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries[token] = &revealEntry{
		encryptedCredential: enc,
		expiresAt:           m.now().Add(ttl),
		consumed:            false,
	}

	return token, nil
}

// Consume retrieves and decrypts the credential once. Subsequent calls fail.
func (m *RevealManager) Consume(token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[token]
	if !ok {
		return "", errors.New("reveal: token not found")
	}

	if entry.consumed {
		return "", errors.New("reveal: token already consumed")
	}

	if m.now().After(entry.expiresAt) {
		entry.consumed = true
		return "", errors.New("reveal: token expired")
	}

	entry.consumed = true
	plain, err := crypto.Decrypt(entry.encryptedCredential, m.kek)
	if err != nil {
		return "", fmt.Errorf("reveal: decrypt: %w", err)
	}

	return plain, nil
}

// ConsumeRevealToken attempts to consume a reveal token across all registered managers.
func ConsumeRevealToken(token string) (string, error) {
	globalRevealMu.RLock()
	managers := make([]*RevealManager, len(globalRevealManagers))
	copy(managers, globalRevealManagers)
	globalRevealMu.RUnlock()

	for _, mgr := range managers {
		cred, err := mgr.Consume(token)
		if err == nil {
			return cred, nil
		}
		if errors.Is(err, errors.New("reveal: token already consumed")) || err.Error() == "reveal: token already consumed" {
			return "", err
		}
		if errors.Is(err, errors.New("reveal: token expired")) || err.Error() == "reveal: token expired" {
			return "", err
		}
	}

	return "", errors.New("reveal: token not found")
}

func (s *Service) getRevealManager() *RevealManager {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revealMgr == nil {
		var kek string
		if s.cfg != nil && !s.cfg.CredentialKEK.IsZero() {
			kek = s.cfg.CredentialKEK.Expose()
		}
		s.revealMgr = NewRevealManager(kek)
	}
	return s.revealMgr
}

// SetRevealManager overrides the reveal manager for testing or custom setup.
func (s *Service) SetRevealManager(mgr *RevealManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revealMgr = mgr
}

// generateRevealURL creates a single-use reveal URL with a 15-minute TTL for the given credential.
// If credential is empty, it returns an empty string.
func (s *Service) generateRevealURL(credential string) string {
	if credential == "" {
		return ""
	}

	mgr := s.getRevealManager()
	token, err := mgr.Store(credential, 15*time.Minute)
	if err != nil {
		slog.Error("failed to store credential for reveal", "err", err)
		return ""
	}

	baseURL := "https://reveal.internal.manova.space"
	if s.cfg != nil && s.cfg.RevealBaseURL != "" {
		baseURL = s.cfg.RevealBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return fmt.Sprintf("%s/reveal?token=%s", baseURL, token)
}

// GenerateRevealURL is the exported version of generateRevealURL.
func (s *Service) GenerateRevealURL(credential string) string {
	return s.generateRevealURL(credential)
}

// ConsumeRevealToken consumes a reveal token using this service's reveal manager.
func (s *Service) ConsumeRevealToken(token string) (string, error) {
	return s.getRevealManager().Consume(token)
}
