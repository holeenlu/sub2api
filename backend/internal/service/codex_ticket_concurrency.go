package service

// Each harvest holds an advisory-lock connection and needs another connection
// for reads/writes. Bound probes and retain at least half the configured pool
// for work other than ticket locks. Manual and background attempts share this cap.
func (s *OpenAIGatewayService) codexTicketConcurrency() int {
	limit := 4
	if s.cfg != nil && s.cfg.Database.MaxOpenConns > 0 {
		limit = min(limit, s.cfg.Database.MaxOpenConns/2)
	}
	return limit
}

func (s *OpenAIGatewayService) acquireCodexTicketAttempt() bool {
	limit := int32(s.codexTicketConcurrency())
	for {
		active := s.openaiCodexTicketActive.Load()
		if active >= limit {
			return false
		}
		if s.openaiCodexTicketActive.CompareAndSwap(active, active+1) {
			return true
		}
	}
}
