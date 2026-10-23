package openai

import "time"

// Consume removes the exact validated session before contacting the upstream.
func (s *SessionStore) Consume(id string, expected *OAuthSession) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[id]
	if !ok || current != expected || time.Since(current.CreatedAt) > SessionTTL {
		return false
	}
	delete(s.sessions, id)
	return true
}
