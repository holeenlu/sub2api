//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

type RedeemCodeRepoSuite struct {
	suite.Suite
	ctx    context.Context
	client *dbent.Client
	repo   *redeemCodeRepository
}

func (s *RedeemCodeRepoSuite) SetupTest() {
	tx := testEntTx(s.T())
	s.ctx = dbent.NewTxContext(context.Background(), tx)
	s.client = tx.Client()
	s.repo = NewRedeemCodeRepository(s.client).(*redeemCodeRepository)
}

func TestRedeemCodeRepoSuite(t *testing.T) {
	suite.Run(t, new(RedeemCodeRepoSuite))
}

func (s *RedeemCodeRepoSuite) createUser(email string) *dbent.User {
	u, err := s.client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		Save(s.ctx)
	s.Require().NoError(err, "create user")
	return u
}

func (s *RedeemCodeRepoSuite) createGroup(name string) *dbent.Group {
	g, err := s.client.Group.Create().
		SetName(name).
		Save(s.ctx)
	s.Require().NoError(err, "create group")
	return g
}

// --- Create / CreateBatch / GetByID / GetByCode ---

func (s *RedeemCodeRepoSuite) TestCreate() {
	expiresAt := time.Now().UTC().Add(2 * time.Hour)
	code := &service.RedeemCode{
		Code:      "TEST-CREATE",
		Type:      service.RedeemTypeBalance,
		Value:     100,
		Status:    service.StatusUnused,
		ExpiresAt: &expiresAt,
	}

	err := s.repo.Create(s.ctx, code)
	s.Require().NoError(err, "Create")
	s.Require().NotZero(code.ID, "expected ID to be set")

	got, err := s.repo.GetByID(s.ctx, code.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().Equal("TEST-CREATE", got.Code)
	s.Require().NotNil(got.ExpiresAt)
	s.Require().WithinDuration(expiresAt, *got.ExpiresAt, time.Second)
}

func (s *RedeemCodeRepoSuite) TestGetByID_NotFound() {
	_, err := s.repo.GetByID(s.ctx, 999999)
	s.Require().Error(err, "expected error for non-existent ID")
	s.Require().ErrorIs(err, service.ErrRedeemCodeNotFound)
}

func (s *RedeemCodeRepoSuite) TestGetByCode() {
	_, err := s.client.RedeemCode.Create().
		SetCode("GET-BY-CODE").
		SetType(service.RedeemTypeBalance).
		SetStatus(service.StatusUnused).
		SetValue(0).
		SetNotes("").
		SetValidityDays(30).
		Save(s.ctx)
	s.Require().NoError(err, "seed redeem code")

	got, err := s.repo.GetByCode(s.ctx, "GET-BY-CODE")
	s.Require().NoError(err, "GetByCode")
	s.Require().Equal("GET-BY-CODE", got.Code)
}

func (s *RedeemCodeRepoSuite) TestGetByCode_NotFound() {
	_, err := s.repo.GetByCode(s.ctx, "NON-EXISTENT")
	s.Require().Error(err, "expected error for non-existent code")
	s.Require().ErrorIs(err, service.ErrRedeemCodeNotFound)
}

// --- Delete ---

// --- List / ListWithFilters ---

// --- Update ---

// --- Use ---

func (s *RedeemCodeRepoSuite) TestUse() {
	user := s.createUser(uniqueTestValue(s.T(), "use") + "@example.com")
	code := &service.RedeemCode{Code: "USE-ME", Type: service.RedeemTypeBalance, Value: 0, Status: service.StatusUnused}
	s.Require().NoError(s.repo.Create(s.ctx, code))

	err := s.repo.Use(s.ctx, code.ID, user.ID)
	s.Require().NoError(err, "Use")

	got, err := s.repo.GetByID(s.ctx, code.ID)
	s.Require().NoError(err)
	s.Require().Equal(service.StatusUsed, got.Status)
	s.Require().NotNil(got.UsedBy)
	s.Require().Equal(user.ID, *got.UsedBy)
	s.Require().NotNil(got.UsedAt)
}

func (s *RedeemCodeRepoSuite) TestUse_Idempotency() {
	user := s.createUser(uniqueTestValue(s.T(), "idem") + "@example.com")
	code := &service.RedeemCode{Code: "IDEM-CODE", Type: service.RedeemTypeBalance, Value: 0, Status: service.StatusUnused}
	s.Require().NoError(s.repo.Create(s.ctx, code))

	err := s.repo.Use(s.ctx, code.ID, user.ID)
	s.Require().NoError(err, "Use first time")

	// Second use should fail
	err = s.repo.Use(s.ctx, code.ID, user.ID)
	s.Require().Error(err, "Use expected error on second call")
	s.Require().ErrorIs(err, service.ErrRedeemCodeUsed)
}

func (s *RedeemCodeRepoSuite) TestUse_AlreadyUsed() {
	user := s.createUser(uniqueTestValue(s.T(), "already") + "@example.com")
	code := &service.RedeemCode{Code: "ALREADY-USED", Type: service.RedeemTypeBalance, Value: 0, Status: service.StatusUsed}
	s.Require().NoError(s.repo.Create(s.ctx, code))

	err := s.repo.Use(s.ctx, code.ID, user.ID)
	s.Require().Error(err, "expected error for already used code")
	s.Require().ErrorIs(err, service.ErrRedeemCodeUsed)
}

// --- ListByUser ---

// --- Combined original test ---
