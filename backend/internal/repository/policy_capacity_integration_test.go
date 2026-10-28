//go:build integration

package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"sync"
	"time"
)

func (s *AccountRepoSuite) TestPolicyCapacityAtomicRenewal() {
	account := mustCreateAccount(s.T(), s.client, &service.Account{Name: "policy-capacity", Extra: map[string]any{"keep": "untouched"}})
	latest := time.Now().Add(time.Hour).UnixMilli()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := int64(0); i < 8; i++ {
		wg.Add(1)
		go func(offset int64) {
			defer wg.Done()
			errs <- s.repo.UpdateExtra(s.ctx, account.ID, map[string]any{service.PolicyCapacityUnavailableUntilKey: latest - offset*1000})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		s.Require().NoError(err)
	}
	// An older completion after the newer observation cannot shorten its lifetime.
	s.Require().NoError(s.repo.UpdateExtra(s.ctx, account.ID, map[string]any{service.PolicyCapacityUnavailableUntilKey: latest - 60000}))
	got, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	s.Require().Equal(float64(latest), got.Extra[service.PolicyCapacityUnavailableUntilKey])
	s.Require().Equal("untouched", got.Extra["keep"])
	s.Require().NotNil(got.PolicyCapacityUnavailableUntil(time.UnixMilli(latest - 1)))
	s.Require().Nil(got.PolicyCapacityUnavailableUntil(time.UnixMilli(latest)))
	var outboxCount int
	s.Require().NoError(scanSingleRow(s.ctx, s.repo.sql, "SELECT COUNT(*) FROM scheduler_outbox", nil, &outboxCount))
	s.Require().Zero(outboxCount, "passive markers must not change scheduling")
	// A normal metadata edit using an old snapshot cannot erase the runtime signal.
	got.Extra = map[string]any{"keep": "edited"}
	s.Require().NoError(s.repo.Update(s.ctx, got))
	edited, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	s.Require().Equal(float64(latest), edited.Extra[service.PolicyCapacityUnavailableUntilKey])

}

func (s *AccountRepoSuite) TestPolicyCapacityNewAccountDoesNotInheritMarker() {
	sourceExtra := map[string]any{service.PolicyCapacityUnavailableUntilKey: time.Now().Add(time.Hour).UnixMilli(), "keep": "setting"}
	account := &service.Account{Name: "policy-copy", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{}, Extra: sourceExtra, Concurrency: 1, Priority: 50, Schedulable: true}
	s.Require().NoError(s.repo.Create(s.ctx, account))
	created, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	s.Require().NotContains(created.Extra, service.PolicyCapacityUnavailableUntilKey)
	s.Require().Contains(sourceExtra, service.PolicyCapacityUnavailableUntilKey, "do not mutate the source account map")
	s.Require().Equal("setting", created.Extra["keep"])
}
