package service

import "context"

type ModelCatalogComparison struct {
	Legacy        *GroupModelCatalog          `json:"legacy"`
	Proposed      *GroupModelCatalog          `json:"proposed"`
	Added         []string                    `json:"added"`
	Removed       []string                    `json:"removed"`
	Routes        map[string]map[int64]string `json:"routes"`
	PriceRevision string                      `json:"price_revision"`
	Enforced      bool                        `json:"enforced"`
}

func (s *ModelCatalogService) Explain(ctx context.Context, g *Group) (*ModelCatalogComparison, error) {
	if s.groupCatalog == nil {
		return nil, ErrModelCatalogUnavailable
	}
	oldGroup := *g
	oldGroup.ModelAllowlist.Mode = "legacy"
	previous, err := s.groupCatalog.Resolve(ctx, &oldGroup)
	if err != nil {
		return nil, err
	}
	proposed, err := s.groupCatalog.Preview(ctx, g)
	if err != nil {
		return nil, err
	}
	result := &ModelCatalogComparison{Legacy: previous, Proposed: proposed, Added: []string{}, Removed: []string{}, Routes: map[string]map[int64]string{}, Enforced: CatalogEnforced(g)}
	if s.prices != nil {
		result.PriceRevision = s.prices.PriceRevision()
	}
	oldIDs, newIDs := map[string]bool{}, map[string]bool{}
	for _, id := range previous.ModelIDs() {
		oldIDs[id] = true
	}
	for _, id := range proposed.ModelIDs() {
		newIDs[id] = true
		if !oldIDs[id] {
			result.Added = append(result.Added, id)
		}
	}
	for _, id := range previous.ModelIDs() {
		if !newIDs[id] {
			result.Removed = append(result.Removed, id)
		}
	}
	for _, m := range proposed.Models {
		result.Routes[m.Platform+":"+m.Name+":"+m.Endpoint] = m.AccountModels
	}
	return result, nil
}

func (s *ModelCatalogService) PricingAudits(ctx context.Context, pending bool) ([]map[string]any, error) {
	return s.repo.PricingAudits(ctx, pending)
}
