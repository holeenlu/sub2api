package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"
)

type modelCatalogSnapshot struct {
	Body           []byte
	Revision       [32]byte
	SourceRevision [32]byte
	UpdatedAt      time.Time
}

func catalogAccountRevision(account *Account) [32]byte {
	data, _ := json.Marshal(struct {
		Credentials    map[string]any
		Parent         *int64
		Proxy          *int64
		Platform, Type string
		Passthrough    bool
	}{account.Credentials, account.ParentAccountID, account.ProxyID, account.Platform, account.Type, account.IsOpenAIPassthroughEnabled()})
	return sha256.Sum256(data)
}

func (s *OpenAIGatewayService) rememberModelCatalog(account, source *Account, response *OpenAIModelsResponse, codex bool) {
	if s == nil || account == nil || source == nil || response == nil || response.NotModified {
		return
	}
	body, err := standardOpenAIModelsBody(response.Body, codex)
	if err != nil {
		return
	}
	// Bound retained bodies by age even when accounts are deleted or never read again.
	s.catalogSnapshots.Range(func(key, value any) bool {
		if time.Since(value.(modelCatalogSnapshot).UpdatedAt) > 24*time.Hour {
			s.catalogSnapshots.Delete(key)
		}
		return true
	})
	s.catalogSnapshots.Store(account.ID, modelCatalogSnapshot{
		Body: body, Revision: catalogAccountRevision(account),
		SourceRevision: catalogAccountRevision(source), UpdatedAt: time.Now().UTC(),
	})
}

// ModelCatalogSnapshot only reads successful prior discovery. Empty catalogs
// remain authoritative, and credential/source changes invalidate the snapshot.
func (s *OpenAIGatewayService) ModelCatalogSnapshot(ctx context.Context, account *Account) ([]string, time.Time, bool) {
	if s == nil || account == nil {
		return nil, time.Time{}, false
	}
	value, ok := s.catalogSnapshots.Load(account.ID)
	if !ok {
		return nil, time.Time{}, false
	}
	snapshot := value.(modelCatalogSnapshot)
	if snapshot.Revision != catalogAccountRevision(account) || time.Since(snapshot.UpdatedAt) > 24*time.Hour {
		return nil, time.Time{}, false
	}
	if account.IsShadow() && s.accountRepo == nil {
		return nil, time.Time{}, false
	}
	source, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil || source == nil || snapshot.SourceRevision != catalogAccountRevision(source) {
		return nil, time.Time{}, false
	}
	body, err := projectAccountModelsBody(snapshot.Body, account, nil, false)
	if err != nil {
		return nil, time.Time{}, false
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return nil, time.Time{}, false
	}
	ids := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		ids = append(ids, m.ID)
	}
	return ids, snapshot.UpdatedAt, true
}
