package jira_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nikhil-sharma-b/jira-tui/internal/jira"
)

// step is one scripted response. A fixture name replays a recorded payload;
// otherwise status and retryAfter are synthesised, because a 5xx is not
// something we can capture from a healthy Jira instance.
type step struct {
	fixture    string
	status     int
	retryAfter string
}

// scriptedServer answers a sequence of responses, one per request, so a test
// can drive a retry loop through its whole shape. The final step repeats once
// the script runs out, which is what an outage looks like.
type scriptedServer struct {
	*httptest.Server

	mu    sync.Mutex
	count int
	// gate, when non-nil, holds each handler until closed, so a test can
	// observe requests that are genuinely simultaneous. Set once at
	// construction and never written again, so the handler reads it unlocked.
	gate chan struct{}
	// arrived receives once per request that reaches the handler.
	arrived  chan struct{}
	inFlight int
	maxSeen  int
}

func newScriptedServer(t *testing.T, steps ...step) *scriptedServer {
	return newGatedServer(t, nil, steps...)
}

// newGatedServer is newScriptedServer with a gate: every handler parks until
// the gate is closed, so a test can hold requests open and observe how many
// are genuinely simultaneous.
func newGatedServer(t *testing.T, gate chan struct{}, steps ...step) *scriptedServer {
	t.Helper()
	s := &scriptedServer{arrived: make(chan struct{}, 64), gate: gate}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		i := s.count
		s.count++
		s.inFlight++
		if s.inFlight > s.maxSeen {
			s.maxSeen = s.inFlight
		}
		s.mu.Unlock()

		select {
		case s.arrived <- struct{}{}:
		default:
		}
		if s.gate != nil {
			<-s.gate
		}

		if i >= len(steps) {
			i = len(steps) - 1
		}
		st := steps[i]
		if st.fixture != "" {
			writeFixture(t, w, st.fixture)
		} else {
			if st.retryAfter != "" {
				w.Header().Set("Retry-After", st.retryAfter)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(st.status)
			_, _ = w.Write([]byte(`{"errorMessages":["scripted"]}`))
		}

		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}))
	t.Cleanup(s.Server.Close)
	return s
}

func (s *scriptedServer) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *scriptedServer) maxConcurrent() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxSeen
}

// expireImmediately is a backoff seam that returns an already-expired timer,
// so a retry proceeds without the suite sleeping.
func expireImmediately(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

// fakeBackoff records the delays a retry asks for and, by default, expires
// them at once: the suite verifies timing by inspecting what was requested,
// never by waiting for it.
type fakeBackoff struct {
	mu      sync.Mutex
	delays  []time.Duration
	blocked chan struct{}
	// hold, when true, returns a channel that never fires, so a test can
	// cancel a request that is mid-backoff.
	hold bool
}

func (f *fakeBackoff) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	f.delays = append(f.delays, d)
	blocked := f.blocked
	hold := f.hold
	f.mu.Unlock()

	if blocked != nil {
		select {
		case blocked <- struct{}{}:
		default:
		}
	}
	if hold {
		return make(chan time.Time)
	}
	return expireImmediately(d)
}

func (f *fakeBackoff) requested() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.delays...)
}

func newTunedClient(t *testing.T, siteURL string, cfg jira.Config) *jira.REST {
	t.Helper()
	cfg.SiteURL = siteURL
	cfg.Email = "someone@example.com"
	cfg.Token = "a-token"
	c, err := jira.NewREST(cfg)
	if err != nil {
		t.Fatalf("NewREST: %v", err)
	}
	return c
}

func TestRetryableReadsRetryWithExponentialBackoff(t *testing.T) {
	tests := []struct {
		name  string
		steps []step
	}{
		{"server error", []step{{status: 500}, {status: 502}, {fixture: "myself.200"}}},
		{"rate limited without a stated delay", []step{{status: 429}, {status: 429}, {fixture: "myself.200"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newScriptedServer(t, tt.steps...)
			clock := &fakeBackoff{}
			c := newTunedClient(t, srv.URL, jira.Config{MaxRetries: 3, After: clock.After})

			if _, err := c.Myself(context.Background()); err != nil {
				t.Fatalf("Myself: %v", err)
			}
			if got, want := srv.requests(), 3; got != want {
				t.Errorf("sent %d requests, want %d", got, want)
			}
			want := []time.Duration{500 * time.Millisecond, time.Second}
			got := clock.requested()
			if len(got) != len(want) {
				t.Fatalf("backoff delays = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("backoff delay %d = %v, want %v", i, got[i], want[i])
				}
			}
		})
	}
}

func TestServerStatedDelayIsHonouredOverComputedBackoff(t *testing.T) {
	// The 429 fixture carries Retry-After: 7, which must win over the 500ms
	// first backoff.
	srv := newScriptedServer(t, step{fixture: "ratelimited.429"}, step{fixture: "myself.200"})
	clock := &fakeBackoff{}
	c := newTunedClient(t, srv.URL, jira.Config{After: clock.After})

	if _, err := c.Myself(context.Background()); err != nil {
		t.Fatalf("Myself: %v", err)
	}
	got := clock.requested()
	if len(got) != 1 || got[0] != 7*time.Second {
		t.Errorf("backoff delays = %v, want [7s] as the server stated", got)
	}
}

func TestClientErrorsAreNotRetried(t *testing.T) {
	for _, fixture := range []string{"unauthorized.401", "forbidden.403", "notfound.404"} {
		t.Run(fixture, func(t *testing.T) {
			srv := newScriptedServer(t, step{fixture: fixture})
			clock := &fakeBackoff{}
			c := newTunedClient(t, srv.URL, jira.Config{After: clock.After})

			if _, err := c.Myself(context.Background()); err == nil {
				t.Fatal("Myself succeeded, want an error")
			}
			if got := srv.requests(); got != 1 {
				t.Errorf("sent %d requests, want 1: this status is not retryable", got)
			}
			if got := clock.requested(); len(got) != 0 {
				t.Errorf("waited %v before giving up, want no wait at all", got)
			}
		})
	}
}

func TestWritesAreNeverRetried(t *testing.T) {
	tests := []struct {
		name string
		step step
	}{
		{"server error", step{status: 500}},
		{"rate limited with a stated delay", step{status: 429, retryAfter: "1"}},
		{"unavailable", step{status: 503}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newScriptedServer(t, tt.step)
			clock := &fakeBackoff{}
			c := newTunedClient(t, srv.URL, jira.Config{MaxRetries: 3, After: clock.After})

			err := c.WriteForTest(context.Background(), "/write", map[string]string{"body": "hello"})
			if err == nil {
				t.Fatal("write succeeded, want an error")
			}
			if got := srv.requests(); got != 1 {
				t.Errorf("sent %d requests, want 1: a write is never repeated", got)
			}
			if got := clock.requested(); len(got) != 0 {
				t.Errorf("waited %v before giving up, want no wait at all", got)
			}
		})
	}
}

func TestRetriesStopAtTheConfiguredCeiling(t *testing.T) {
	srv := newScriptedServer(t, step{status: 503})
	clock := &fakeBackoff{}
	c := newTunedClient(t, srv.URL, jira.Config{MaxRetries: 2, After: clock.After})

	_, err := c.Myself(context.Background())
	if err == nil {
		t.Fatal("Myself succeeded, want an error")
	}
	if !jira.HasStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("error %v does not carry the last status the server sent", err)
	}
	if got, want := srv.requests(), 3; got != want {
		t.Errorf("sent %d requests, want %d: one attempt plus two retries", got, want)
	}
}

func TestOfflineIsNotRetried(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	clock := &fakeBackoff{}
	c := newTunedClient(t, url, jira.Config{MaxRetries: 3, After: clock.After})

	_, err := c.Myself(context.Background())
	if !jira.IsOffline(err) {
		t.Fatalf("IsOffline = false for %v, want true", err)
	}
	if got := clock.requested(); len(got) != 0 {
		t.Errorf("backed off %v before reporting offline, want none: the UI shows the marker at once", got)
	}
}

func TestConcurrentRequestsAreCapped(t *testing.T) {
	const limit = 2
	gate := make(chan struct{})
	srv := newGatedServer(t, gate, step{fixture: "myself.200"})
	c := newTunedClient(t, srv.URL, jira.Config{MaxConcurrent: limit})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Myself(context.Background()); err != nil {
				t.Errorf("Myself: %v", err)
			}
		}()
	}

	// Once every slot is parked in the handler no further request can reach
	// it, so an empty arrival channel here is proof of the ceiling rather than
	// a race with a slow goroutine.
	for i := 0; i < limit; i++ {
		<-srv.arrived
	}
	if extra := len(srv.arrived); extra != 0 {
		t.Errorf("%d requests beyond the cap of %d reached the server", extra, limit)
	}
	close(gate)
	wg.Wait()

	// Exactly the cap, not merely at most it: fewer would mean the client had
	// serialised the requests, which is a different bug wearing the same
	// passing assertion.
	if got := srv.maxConcurrent(); got != limit {
		t.Errorf("saw %d simultaneous requests, want %d", got, limit)
	}
	if got, want := srv.requests(), 10; got != want {
		t.Errorf("server handled %d requests, want %d: requests beyond the cap wait, they do not fail", got, want)
	}
}

func TestCancelledContextAbandonsARequestWaitingForASlot(t *testing.T) {
	gate := make(chan struct{})
	srv := newGatedServer(t, gate, step{fixture: "myself.200"})
	c := newTunedClient(t, srv.URL, jira.Config{MaxConcurrent: 1})

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := c.Myself(context.Background()); err != nil {
			t.Errorf("Myself: %v", err)
		}
	}()
	<-srv.arrived // the only slot is now held

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Myself(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not unwrap to context.Canceled", err)
	}
	if jira.IsOffline(err) {
		t.Error("IsOffline = true, want false: the caller cancelled while queued")
	}

	close(gate)
	<-done
	if got, want := srv.requests(), 1; got != want {
		t.Errorf("server handled %d requests, want %d: the cancelled one never went out", got, want)
	}
}

func TestCancelledContextAbandonsARequestInFlight(t *testing.T) {
	// The handler parks until the gate opens, so the cancellation lands while
	// the round trip is genuinely outstanding.
	gate := make(chan struct{})
	srv := newGatedServer(t, gate, step{status: 500})
	defer close(gate)
	c := newTunedClient(t, srv.URL, jira.Config{MaxRetries: 3, After: expireImmediately})

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := c.Myself(ctx)
		errc <- err
	}()

	<-srv.arrived
	cancel()

	err := <-errc
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not unwrap to context.Canceled", err)
	}
	if jira.IsOffline(err) {
		t.Error("IsOffline = true, want false: the caller cancelled, the site did not fail")
	}
}

func TestCancelledContextAbandonsABackoffWait(t *testing.T) {
	srv := newScriptedServer(t, step{status: 503})
	clock := &fakeBackoff{blocked: make(chan struct{}, 1), hold: true}
	c := newTunedClient(t, srv.URL, jira.Config{MaxRetries: 3, After: clock.After})

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := c.Myself(ctx)
		errc <- err
	}()

	<-clock.blocked // the request is parked in backoff
	cancel()

	err := <-errc
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not unwrap to context.Canceled", err)
	}
	if got, want := srv.requests(), 1; got != want {
		t.Errorf("sent %d requests, want %d: the retry was abandoned", got, want)
	}
}
