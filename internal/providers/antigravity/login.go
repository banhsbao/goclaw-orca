package antigravity

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const (
	menuMarker = "select login method"
	codeMarker = "paste the authorization code below"
	maxCodeLen = 512
)

type Proc struct {
	Stdin io.Writer
	Done  <-chan struct{}
	Kill  func()
}

type starterFunc func(id uuid.UUID, out io.Writer) (*Proc, error)

type authedFunc func(ctx context.Context, id uuid.UUID) (bool, error)

type loginSession struct {
	mu    sync.Mutex
	out   safeBuf
	state sync.Mutex
	proc  *Proc
	timer *time.Timer
	url   string
	dead  bool
	kill  sync.Once
}

func (s *loginSession) attach(p *Proc) bool {
	s.state.Lock()
	defer s.state.Unlock()
	if s.dead {
		return false
	}
	s.proc = p
	return true
}

func (s *loginSession) isDead() bool {
	s.state.Lock()
	defer s.state.Unlock()
	return s.dead
}

func (s *loginSession) shutdown() {
	s.state.Lock()
	s.dead = true
	if s.timer != nil {
		s.timer.Stop()
	}
	p := s.proc
	s.state.Unlock()
	if p != nil {
		s.killProc(p)
	}
}

func (s *loginSession) killProc(p *Proc) {
	s.kill.Do(p.Kill)
}

func (s *loginSession) arm(ttl time.Duration, expire func()) bool {
	s.state.Lock()
	defer s.state.Unlock()
	if s.dead {
		return false
	}
	s.timer = time.AfterFunc(ttl, expire)
	return true
}

type LoginManager struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*loginSession
	spawn    map[uuid.UUID]*sync.Mutex
	start    starterFunc
	authed   authedFunc
	ttl      time.Duration

	startWait   time.Duration
	submitWait  time.Duration
	submitDelay time.Duration
	poll        time.Duration
	authPoll    time.Duration
}

func NewLoginManager() *LoginManager {
	return NewLoginManagerWith(dockerStarter, modelsAuthed, 10*time.Minute)
}

func NewLoginManagerWith(start starterFunc, authed authedFunc, ttl time.Duration) *LoginManager {
	return &LoginManager{
		sessions:    map[uuid.UUID]*loginSession{},
		spawn:       map[uuid.UUID]*sync.Mutex{},
		start:       start,
		authed:      authed,
		ttl:         ttl,
		startWait:   45 * time.Second,
		submitWait:  45 * time.Second,
		submitDelay: time.Second,
		poll:        200 * time.Millisecond,
		authPoll:    2 * time.Second,
	}
}

func modelsAuthed(ctx context.Context, id uuid.UUID) (bool, error) {
	_, err := Models(ctx, id)
	return err == nil, nil
}

func (m *LoginManager) drop(id uuid.UUID, s *loginSession) {
	m.mu.Lock()
	if m.sessions[id] == s {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	s.shutdown()
}

func (m *LoginManager) Cancel(id uuid.UUID) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s != nil {
		s.shutdown()
	}
}

func (m *LoginManager) Start(ctx context.Context, id uuid.UUID) (string, bool, error) {
	if ok, _ := m.authed(ctx, id); ok {
		return "", true, nil
	}
	s := &loginSession{}
	s.mu.Lock()
	defer s.mu.Unlock()
	m.mu.Lock()
	old := m.sessions[id]
	m.sessions[id] = s
	m.mu.Unlock()
	if old != nil {
		old.shutdown()
	}

	proc, err := m.spawnProc(id, s)
	if err != nil {
		m.drop(id, s)
		return "", false, err
	}
	if !s.attach(proc) {
		s.killProc(proc)
		return "", false, fmt.Errorf("%w: superseded by a newer login", ErrLoginFailed)
	}

	menuSent := false
	authURL, err := m.waitFor(ctx, s, m.startWait, func(txt string) (string, bool) {
		low := strings.ToLower(txt)
		if !menuSent && strings.Contains(low, menuMarker) {
			menuSent = true
			_, _ = proc.Stdin.Write([]byte("\r"))
		}
		if strings.Contains(low, codeMarker) {
			if u := extractAuthURL(txt); u != "" {
				return u, true
			}
		}
		return "", false
	})
	if err != nil {
		m.drop(id, s)
		return "", false, err
	}
	s.url = authURL
	if !s.arm(m.ttl, func() { m.drop(id, s) }) {
		return "", false, fmt.Errorf("%w: superseded by a newer login", ErrLoginFailed)
	}
	return authURL, false, nil
}

func (m *LoginManager) waitFor(ctx context.Context, s *loginSession, d time.Duration, cond func(string) (string, bool)) (string, error) {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(m.poll)
	defer tick.Stop()
	for {
		txt := stripANSI(s.out.String())
		if v, ok := cond(txt); ok {
			return v, nil
		}
		if s.isDead() {
			return "", fmt.Errorf("%w: superseded by a newer login", ErrLoginFailed)
		}
		select {
		case <-s.proc.Done:
			return "", fmt.Errorf("%w: %s", ErrLoginFailed, tail(stripANSI(s.out.String())))
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("%w: timed out waiting for login URL: %s", ErrLoginFailed, tail(txt))
		case <-tick.C:
		}
	}
}

func validCode(code string) bool {
	if code == "" || len(code) > maxCodeLen {
		return false
	}
	for _, r := range code {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func (m *LoginManager) SubmitCode(ctx context.Context, id uuid.UUID, code string) (bool, error) {
	code = strings.TrimSpace(code)
	if !validCode(code) {
		return false, ErrInvalidCode
	}
	m.mu.Lock()
	s := m.sessions[id]
	m.mu.Unlock()
	if s == nil {
		return false, ErrNoLoginSession
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Lock()
	proc, dead := s.proc, s.dead
	s.state.Unlock()
	if proc == nil || dead {
		return false, ErrNoLoginSession
	}

	if _, err := proc.Stdin.Write([]byte(code)); err != nil {
		return false, fmt.Errorf("%w: %v", ErrLoginFailed, err)
	}
	select {
	case <-time.After(m.submitDelay):
	case <-proc.Done:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if _, err := proc.Stdin.Write([]byte("\r")); err != nil {
		return false, fmt.Errorf("%w: %v", ErrLoginFailed, err)
	}

	deadline := time.NewTimer(m.submitWait)
	defer deadline.Stop()
	tick := time.NewTicker(m.authPoll)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-proc.Done:
		case <-deadline.C:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
		if ok, _ := m.authed(ctx, id); ok {
			m.drop(id, s)
			return true, nil
		}
		select {
		case <-proc.Done:
			m.drop(id, s)
			return false, fmt.Errorf("%w: %s", ErrLoginFailed, tail(stripANSI(s.out.String())))
		default:
		}
	}
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}

type safeBuf struct {
	mu sync.Mutex
	b  []byte
}

func (b *safeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.b = append(b.b, p...)
	if len(b.b) > 1<<20 {
		b.b = append([]byte(nil), b.b[len(b.b)-(1<<19):]...)
	}
	return len(p), nil
}

func (b *safeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.b)
}

func (m *LoginManager) spawnLock(id uuid.UUID) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.spawn[id]
	if l == nil {
		l = &sync.Mutex{}
		m.spawn[id] = l
	}
	return l
}

func (m *LoginManager) spawnProc(id uuid.UUID, s *loginSession) (*Proc, error) {
	l := m.spawnLock(id)
	l.Lock()
	defer l.Unlock()
	if s.isDead() {
		return nil, fmt.Errorf("%w: superseded by a newer login", ErrLoginFailed)
	}
	proc, err := m.start(id, &s.out)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLoginFailed, err)
	}
	return proc, nil
}
