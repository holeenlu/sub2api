package service

import "context"

type CatalogTicketCandidate struct {
	Model                string `json:"model"`
	FingerprintAvailable bool   `json:"fingerprint_available"`
	Access               string `json:"access"`
}

func (s *ModelCatalogService) TicketCandidates(ctx context.Context, a *Account) ([]CatalogTicketCandidate, error) {
	snapshot, err := s.Account(ctx, a)
	if err != nil {
		return nil, err
	}
	out := []CatalogTicketCandidate{}
	bank, _ := ModelTraceTicketModels()
	fingerprints := map[string]bool{}
	for _, id := range bank {
		fingerprints[id] = true
	}
	for _, e := range snapshot.Models {
		if e.Kind != "chat" || e.Lifecycle == "retired" || e.Access == "candidate" || e.Access == "unlisted" {
			continue
		}
		if !codexTicketAccountSupportsModel(a, e.ID) {
			continue
		}
		if allowed, handled := accountCatalogPolicyAllows(a, e.ID); handled && !allowed {
			continue
		}
		out = append(out, CatalogTicketCandidate{e.ID, fingerprints[normalizeOpenAICodexTicketModel(e.ID)], e.Access})
	}
	return out, nil
}
