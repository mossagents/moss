package session

import (
	"encoding/json"
	"strings"
)

// StateScope identifies which scope a state key belongs to.
type StateScope string

const (
	// StateScopeSession is the default scope, per-session state.
	StateScopeSession StateScope = ""
	// StateScopeApp is application-level state shared across sessions.
	StateScopeApp StateScope = "app"
	// StateScopeUser is user-level state shared across sessions for a user.
	StateScopeUser StateScope = "user"
	// StateScopeTemp is temporary state cleared after each invocation.
	StateScopeTemp StateScope = "temp"
)

// ScopedState holds four independent state maps corresponding to the four
// scopes. The Session's mutex protects all scopes; sharing across session
// clones (e.g. App/User persistence) is a SessionStore concern.
type ScopedState struct {
	Session map[string]any `json:"session,omitempty"`
	App     map[string]any `json:"app,omitempty"`
	User    map[string]any `json:"user,omitempty"`
	Temp    map[string]any `json:"temp,omitempty"`
}

// scopeMarker is a JSON-level marker that distinguishes the new scoped format
// from the legacy flat-map format on disk.
const scopeMarker = "_scoped"

// ParseScopeKey splits a key with an optional "scope:" prefix into (scope, realKey).
// Keys without a recognised prefix are assigned to StateScopeSession.
//
//	"foo"       → (StateScopeSession, "foo")
//	"app:foo"   → (StateScopeApp, "foo")
//	"user:foo"  → (StateScopeUser, "foo")
//	"temp:foo"  → (StateScopeTemp, "foo")
func ParseScopeKey(key string) (StateScope, string) {
	idx := strings.IndexByte(key, ':')
	if idx < 0 {
		return StateScopeSession, key
	}
	prefix := key[:idx]
	switch StateScope(prefix) {
	case StateScopeApp:
		return StateScopeApp, key[idx+1:]
	case StateScopeUser:
		return StateScopeUser, key[idx+1:]
	case StateScopeTemp:
		return StateScopeTemp, key[idx+1:]
	default:
		// Unknown prefix — treat the whole string as a session-scope key.
		return StateScopeSession, key
	}
}

// scopeMap returns the map for the given scope, initialising it if needed.
func (s *ScopedState) scopeMap(scope StateScope, init bool) map[string]any {
	switch scope {
	case StateScopeApp:
		if init && s.App == nil {
			s.App = make(map[string]any)
		}
		return s.App
	case StateScopeUser:
		if init && s.User == nil {
			s.User = make(map[string]any)
		}
		return s.User
	case StateScopeTemp:
		if init && s.Temp == nil {
			s.Temp = make(map[string]any)
		}
		return s.Temp
	default:
		if init && s.Session == nil {
			s.Session = make(map[string]any)
		}
		return s.Session
	}
}

// Set writes a value to the specified scope.
func (s *ScopedState) Set(scope StateScope, key string, value any) {
	m := s.scopeMap(scope, true)
	m[key] = value
}

// Get reads a value from the specified scope.
func (s *ScopedState) Get(scope StateScope, key string) (any, bool) {
	m := s.scopeMap(scope, false)
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

// Delete removes a key from the specified scope.
func (s *ScopedState) Delete(scope StateScope, key string) {
	m := s.scopeMap(scope, false)
	delete(m, key)
}

// ClearTemp resets the Temp scope.
func (s *ScopedState) ClearTemp() {
	s.Temp = nil
}

// CopySessionScope returns a shallow copy of the Session scope map.
func (s *ScopedState) CopySessionScope() map[string]any {
	if len(s.Session) == 0 {
		return nil
	}
	out := make(map[string]any, len(s.Session))
	for k, v := range s.Session {
		out[k] = v
	}
	return out
}

// Clone returns a deep copy of all four scopes (each map is shallow-copied).
func (s *ScopedState) Clone() ScopedState {
	return ScopedState{
		Session: shallowCopyMap(s.Session),
		App:     shallowCopyMap(s.App),
		User:    shallowCopyMap(s.User),
		Temp:    shallowCopyMap(s.Temp),
	}
}

// IsEmpty returns true if all scopes are empty.
func (s *ScopedState) IsEmpty() bool {
	return len(s.Session) == 0 && len(s.App) == 0 && len(s.User) == 0 && len(s.Temp) == 0
}

func shallowCopyMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// MarshalJSON writes the scoped state with a "_scoped" marker so that legacy
// flat-map state can be distinguished on unmarshal.
func (s ScopedState) MarshalJSON() ([]byte, error) {
	if s.IsEmpty() {
		return []byte("null"), nil
	}
	type wire struct {
		Scoped  bool           `json:"_scoped"`
		Session map[string]any `json:"session,omitempty"`
		App     map[string]any `json:"app,omitempty"`
		User    map[string]any `json:"user,omitempty"`
		Temp    map[string]any `json:"temp,omitempty"`
	}
	return json.Marshal(wire{
		Scoped:  true,
		Session: s.Session,
		App:     s.App,
		User:    s.User,
		Temp:    s.Temp,
	})
}

// UnmarshalJSON reads either the new scoped format (with "_scoped" marker) or
// the legacy flat-map format (migrated into the Session scope).
func (s *ScopedState) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*s = ScopedState{}
		return nil
	}

	// Probe for the marker.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}

	if _, ok := probe[scopeMarker]; ok {
		// New format.
		type wire struct {
			Session map[string]any `json:"session,omitempty"`
			App     map[string]any `json:"app,omitempty"`
			User    map[string]any `json:"user,omitempty"`
			Temp    map[string]any `json:"temp,omitempty"`
		}
		var w wire
		if err := json.Unmarshal(data, &w); err != nil {
			return err
		}
		*s = ScopedState{
			Session: w.Session,
			App:     w.App,
			User:    w.User,
			Temp:    w.Temp,
		}
		return nil
	}

	// Legacy flat-map format → migrate into Session scope.
	var flat map[string]any
	if err := json.Unmarshal(data, &flat); err != nil {
		return err
	}
	*s = ScopedState{Session: flat}
	return nil
}
