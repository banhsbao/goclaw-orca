package antigravity

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeProc struct {
	mu      sync.Mutex
	writes  []string
	killed  atomic.Bool
	kills   atomic.Int32
	done    chan struct{}
	once    sync.Once
	out     io.Writer
	hasMenu bool
}

func (f *fakeProc) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, string(p))
	if string(p) == "\r" && f.hasMenu && len(f.writes) == 1 {
		_, _ = f.out.Write([]byte("\x1b[1mLogin\x1b[0m\r\n" + goodURL + "\r\nPlease paste the authorization code below\r\n"))
	}
	return len(p), nil
}

func (f *fakeProc) written() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

func (f *fakeProc) exit() { f.once.Do(func() { close(f.done) }) }

type harness struct {
	m      *LoginManager
	mu     sync.Mutex
	procs  []*fakeProc
	authed atomic.Bool
	spawns atomic.Int32
	menu   bool
	delay  time.Duration
}

func newHarness(menu bool) *harness {
	h := &harness{menu: menu}
	start := func(id uuid.UUID, out io.Writer) (*Proc, error) {
		h.spawns.Add(1)
		if h.delay > 0 {
			time.Sleep(h.delay)
		}
		f := &fakeProc{done: make(chan struct{}), out: out, hasMenu: menu}
		h.mu.Lock()
		h.procs = append(h.procs, f)
		h.mu.Unlock()
		if menu {
			_, _ = out.Write([]byte("Select login method\r\n"))
		}
		return &Proc{Stdin: f, Done: f.done, Kill: func() { f.kills.Add(1); f.killed.Store(true); f.exit() }}, nil
	}
	authed := func(context.Context, uuid.UUID) (bool, error) { return h.authed.Load(), nil }
	h.m = NewLoginManagerWith(start, authed, time.Minute)
	h.m.poll = 2 * time.Millisecond
	h.m.authPoll = 5 * time.Millisecond
	h.m.submitDelay = time.Millisecond
	h.m.startWait = time.Second
	h.m.submitWait = 200 * time.Millisecond
	return h
}

func (h *harness) proc(i int) *fakeProc {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.procs[i]
}

func TestStart_AlreadyAuthenticated(t *testing.T) {
	h := newHarness(true)
	h.authed.Store(true)
	_, already, err := h.m.Start(context.Background(), uuid.New())
	if err != nil || !already {
		t.Fatalf("already=%v err=%v", already, err)
	}
	if h.spawns.Load() != 0 {
		t.Error("spawned a process")
	}
}

func TestStart_SendsEnterAndReturnsURL(t *testing.T) {
	h := newHarness(true)
	url, already, err := h.m.Start(context.Background(), uuid.New())
	if err != nil || already || url != goodURL {
		t.Fatalf("url=%q already=%v err=%v", url, already, err)
	}
	if w := h.proc(0).written(); len(w) != 1 || w[0] != "\r" {
		t.Errorf("writes = %q", w)
	}
}

func TestStart_ProcessExitsBeforeURL(t *testing.T) {
	h := newHarness(false)
	go func() {
		time.Sleep(30 * time.Millisecond)
		h.proc(0).exit()
	}()
	_, _, err := h.m.Start(context.Background(), uuid.New())
	if !errors.Is(err, ErrLoginFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestSubmitCode_NoSession(t *testing.T) {
	h := newHarness(true)
	if _, err := h.m.SubmitCode(context.Background(), uuid.New(), "abc"); !errors.Is(err, ErrNoLoginSession) {
		t.Fatalf("err = %v", err)
	}
}

func TestSubmitCode_InvalidCodeWritesNothing(t *testing.T) {
	h := newHarness(true)
	id := uuid.New()
	if _, _, err := h.m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"", "a\nb", "a\x01b", "a b", strings.Repeat("x", 513)} {
		if _, err := h.m.SubmitCode(context.Background(), id, c); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("code %q: err = %v", c, err)
		}
	}
	if w := h.proc(0).written(); len(w) != 1 {
		t.Errorf("writes = %q", w)
	}
}

func TestSubmitCode_Success(t *testing.T) {
	h := newHarness(true)
	id := uuid.New()
	if _, _, err := h.m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		h.authed.Store(true)
	}()
	ok, err := h.m.SubmitCode(context.Background(), id, "4/code")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	w := h.proc(0).written()
	if len(w) != 3 || w[1] != "4/code" || w[2] != "\r" {
		t.Errorf("writes = %q", w)
	}
	if !h.proc(0).killed.Load() {
		t.Error("process not killed")
	}
	if _, err := h.m.SubmitCode(context.Background(), id, "again"); !errors.Is(err, ErrNoLoginSession) {
		t.Errorf("session still present: %v", err)
	}
}

func TestSubmitCode_PendingKeepsSession(t *testing.T) {
	h := newHarness(true)
	id := uuid.New()
	if _, _, err := h.m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	ok, err := h.m.SubmitCode(context.Background(), id, "4/code")
	if ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if h.proc(0).killed.Load() {
		t.Error("session killed while pending")
	}
}

func TestSubmitCode_ProcessExitWithoutAuth(t *testing.T) {
	h := newHarness(true)
	id := uuid.New()
	if _, _, err := h.m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		h.proc(0).exit()
	}()
	if _, err := h.m.SubmitCode(context.Background(), id, "4/code"); !errors.Is(err, ErrLoginFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestStart_TwiceKillsFirstAndOldTimerSparesSecond(t *testing.T) {
	h := newHarness(true)
	h.m.ttl = 50 * time.Millisecond
	id := uuid.New()
	if _, _, err := h.m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, _, err := h.m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if !h.proc(0).killed.Load() {
		t.Error("first session not killed")
	}
	time.Sleep(30 * time.Millisecond)
	if h.proc(1).killed.Load() {
		t.Error("second session killed by first timer")
	}
	time.Sleep(60 * time.Millisecond)
	if !h.proc(1).killed.Load() {
		t.Error("second session not expired by its own ttl")
	}
}

func TestStart_RequestContextCancelDoesNotKillSession(t *testing.T) {
	h := newHarness(true)
	id := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	if _, _, err := h.m.Start(ctx, id); err != nil {
		t.Fatal(err)
	}
	cancel()
	h.authed.Store(true)
	ok, err := h.m.SubmitCode(context.Background(), id, "4/code")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(h.proc(0).written()) != 3 {
		t.Error("code not written to live session")
	}
}

func TestStart_ConcurrentSameIDLeavesOneLiveProcess(t *testing.T) {
	h := newHarness(true)
	h.delay = 10 * time.Millisecond
	id := uuid.New()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = h.m.Start(context.Background(), id)
		}()
	}
	wg.Wait()
	live := 0
	for i := range int(h.spawns.Load()) {
		if !h.proc(i).killed.Load() {
			live++
		}
	}
	if live != 1 {
		t.Errorf("live processes = %d", live)
	}
}

func TestCancelKillsSession(t *testing.T) {
	h := newHarness(true)
	id := uuid.New()
	if _, _, err := h.m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	h.m.Cancel(id)
	if !h.proc(0).killed.Load() {
		t.Error("not killed")
	}
}

func TestTTLExpiryKills(t *testing.T) {
	h := newHarness(true)
	h.m.ttl = 20 * time.Millisecond
	if _, _, err := h.m.Start(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if !h.proc(0).killed.Load() {
		t.Error("not killed after ttl")
	}
}

func TestSupersededSessionDropKillsOnceAndSparesNewer(t *testing.T) {
	h := newHarness(true)
	id := uuid.New()
	if _, _, err := h.m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	h.m.mu.Lock()
	old := h.m.sessions[id]
	h.m.mu.Unlock()
	if _, _, err := h.m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	h.m.drop(id, old)
	h.m.drop(id, old)
	if got := h.proc(0).kills.Load(); got != 1 {
		t.Errorf("old proc kills = %d, want 1", got)
	}
	if h.proc(1).killed.Load() {
		t.Error("newer session killed by superseded session cleanup")
	}
}

func TestStart_ConcurrentSupersedeKillsOnlyFirst(t *testing.T) {
	h := newHarness(true)
	h.delay = 50 * time.Millisecond
	id := uuid.New()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, _, err := h.m.Start(context.Background(), id)
			errs <- err
		}()
		time.Sleep(10 * time.Millisecond)
	}
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) {
		t.Fatalf("expected exactly one success: %v / %v", e1, e2)
	}
	if h.spawns.Load() != 2 {
		t.Fatalf("spawns = %d, want 2", h.spawns.Load())
	}
	if h.proc(0).kills.Load() != 1 || h.proc(1).killed.Load() {
		t.Errorf("kills first=%d second=%v", h.proc(0).kills.Load(), h.proc(1).killed.Load())
	}
}
