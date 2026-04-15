package distributed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// DistributedLock provides mutual-exclusion across multiple processes
// via a lock-token mechanism.
type DistributedLock interface {
	// Acquire tries to obtain the lock for resource. Returns a non-empty
	// token on success, or an error if the lock is already held.
	Acquire(ctx context.Context, resource string, ttl time.Duration) (token string, err error)
	// Release releases the lock identified by token.
	Release(ctx context.Context, resource, token string) error
	// Refresh extends the TTL of an existing lock.
	Refresh(ctx context.Context, resource, token string, ttl time.Duration) error
}

// ---- InProcessLock (single-process, for tests) ---------------------------

// InProcessLock implements DistributedLock using an in-memory sync.Map.
// Useful for single-process tests and local development.
type InProcessLock struct {
	mu    sync.Mutex
	locks map[string]*lockEntry
}

type lockEntry struct {
	token     string
	expiresAt time.Time
}

// NewInProcessLock creates a new InProcessLock.
func NewInProcessLock() *InProcessLock {
	return &InProcessLock{locks: make(map[string]*lockEntry)}
}

func (l *InProcessLock) Acquire(ctx context.Context, resource string, ttl time.Duration) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.locks[resource]; ok && time.Now().Before(entry.expiresAt) {
		return "", fmt.Errorf("distributed: lock %q already held", resource)
	}
	token := fmt.Sprintf("%s-%d", resource, time.Now().UnixNano())
	l.locks[resource] = &lockEntry{token: token, expiresAt: time.Now().Add(ttl)}
	return token, nil
}

func (l *InProcessLock) Release(ctx context.Context, resource, token string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.locks[resource]
	if !ok || entry.token != token {
		return fmt.Errorf("distributed: lock %q not held by token %q", resource, token)
	}
	delete(l.locks, resource)
	return nil
}

func (l *InProcessLock) Refresh(ctx context.Context, resource, token string, ttl time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.locks[resource]
	if !ok || entry.token != token {
		return fmt.Errorf("distributed: lock %q not held by token %q", resource, token)
	}
	entry.expiresAt = time.Now().Add(ttl)
	return nil
}

// ---- TokenLock (HTTP-backed) --------------------------------------------

// TokenLock implements DistributedLock by calling a remote lock server.
// The server is expected to expose:
//
//	POST /locks/{resource}          body: {"ttl_ms": N}  → {"token": "..."}
//	DELETE /locks/{resource}/{token}                     → 204
//	PUT    /locks/{resource}/{token} body: {"ttl_ms": N} → 204
type TokenLock struct {
	baseURL    string
	httpClient *http.Client
}

// NewTokenLock creates a TokenLock pointing at baseURL.
func NewTokenLock(baseURL string) *TokenLock {
	return &TokenLock{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (l *TokenLock) Acquire(ctx context.Context, resource string, ttl time.Duration) (string, error) {
	var resp struct {
		Token string `json:"token"`
	}
	body := map[string]int64{"ttl_ms": ttl.Milliseconds()}
	if err := doJSON(ctx, l.httpClient, http.MethodPost, l.baseURL+"/locks/"+resource, body, &resp); err != nil {
		return "", err
	}
	return resp.Token, nil
}

func (l *TokenLock) Release(ctx context.Context, resource, token string) error {
	u := fmt.Sprintf("%s/locks/%s/%s", l.baseURL, resource, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	resp, err := l.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("distributed: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("distributed: release status %d", resp.StatusCode)
	}
	return nil
}

func (l *TokenLock) Refresh(ctx context.Context, resource, token string, ttl time.Duration) error {
	u := fmt.Sprintf("%s/locks/%s/%s", l.baseURL, resource, token)
	body := map[string]int64{"ttl_ms": ttl.Milliseconds()}
	return doJSON(ctx, l.httpClient, http.MethodPut, u, body, nil)
}

// ---- LockServer (HTTP server side of TokenLock) -------------------------

// LockServer exposes an HTTP lock service backed by InProcessLock.
// Mount its Handler() at some prefix or use Serve() to run standalone.
type LockServer struct {
	lock *InProcessLock
	mux  *http.ServeMux
}

// NewLockServer creates a new LockServer.
func NewLockServer() *LockServer {
	s := &LockServer{lock: NewInProcessLock(), mux: http.NewServeMux()}
	s.mux.HandleFunc("/locks/", s.handleLock)
	return s
}

// Handler returns the underlying http.Handler.
func (s *LockServer) Handler() http.Handler { return s.mux }

// Serve starts the HTTP lock server on addr (blocking).
func (s *LockServer) Serve(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

func (s *LockServer) handleLock(w http.ResponseWriter, r *http.Request) {
	// path: /locks/{resource} or /locks/{resource}/{token}
	parts := splitPath(r.URL.Path, "/locks/")
	ctx := r.Context()

	switch {
	case len(parts) == 1 && r.Method == http.MethodPost:
		var body struct {
			TTLMs int64 `json:"ttl_ms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		token, err := s.lock.Acquire(ctx, parts[0], time.Duration(body.TTLMs)*time.Millisecond)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})

	case len(parts) == 2 && r.Method == http.MethodDelete:
		if err := s.lock.Release(ctx, parts[0], parts[1]); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case len(parts) == 2 && r.Method == http.MethodPut:
		var body struct {
			TTLMs int64 `json:"ttl_ms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.lock.Refresh(ctx, parts[0], parts[1], time.Duration(body.TTLMs)*time.Millisecond); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.NotFound(w, r)
	}
}

// ---- shared helpers ------------------------------------------------------

func splitPath(full, prefix string) []string {
	tail := full[len(prefix):]
	var parts []string
	for _, p := range splitN(tail, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitN(s, sep string) []string {
	var out []string
	for {
		i := indexOf(s, sep)
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i])
		s = s[i+len(sep):]
	}
	return out
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func doJSON(ctx context.Context, client *http.Client, method, rawURL string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("distributed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		var apiErr struct{ Error string }
		if jerr := json.NewDecoder(resp.Body).Decode(&apiErr); jerr == nil && apiErr.Error != "" {
			return fmt.Errorf("distributed: %s", apiErr.Error)
		}
		return fmt.Errorf("distributed: status %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
