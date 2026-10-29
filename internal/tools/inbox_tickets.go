package tools

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

const (
	InboxTicketTTL      = 30 * time.Minute
	InboxTicketMaxFiles = 10
)

var (
	ErrInboxTicketInvalid = errors.New("inbox ticket invalid or expired")
	ErrInboxTicketUsedUp  = errors.New("inbox ticket has no uploads left")
)

type inboxTicket struct {
	dir       string
	expiresAt time.Time
	remaining int
}

type InboxTicketStore struct {
	mu      sync.Mutex
	tickets map[string]*inboxTicket
	baseURL func() string
	now     func() time.Time
}

func NewInboxTicketStore() *InboxTicketStore {
	return &InboxTicketStore{tickets: make(map[string]*inboxTicket), now: time.Now}
}

func (s *InboxTicketStore) SetBaseURL(fn func() string) {
	s.mu.Lock()
	s.baseURL = fn
	s.mu.Unlock()
}

func (s *InboxTicketStore) BaseURL() string {
	s.mu.Lock()
	fn := s.baseURL
	s.mu.Unlock()
	if fn == nil {
		return ""
	}
	return fn()
}

func (s *InboxTicketStore) Issue(dir string, ttl time.Duration, maxFiles int) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	id := base64.RawURLEncoding.EncodeToString(buf)
	expires := s.now().Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.tickets[id] = &inboxTicket{dir: dir, expiresAt: expires, remaining: maxFiles}
	return id, expires, nil
}

func (s *InboxTicketStore) Consume(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tickets[id]
	if !ok || !s.now().Before(t.expiresAt) {
		delete(s.tickets, id)
		return "", ErrInboxTicketInvalid
	}
	if t.remaining <= 0 {
		return "", ErrInboxTicketUsedUp
	}
	t.remaining--
	return t.dir, nil
}

func (s *InboxTicketStore) Refund(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tickets[id]; ok {
		t.remaining++
	}
}

func (s *InboxTicketStore) pruneLocked() {
	now := s.now()
	for id, t := range s.tickets {
		if !now.Before(t.expiresAt) {
			delete(s.tickets, id)
		}
	}
}
