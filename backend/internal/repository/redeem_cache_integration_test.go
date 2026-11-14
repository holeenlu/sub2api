//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type RedeemCacheSuite struct {
	IntegrationRedisSuite
	cache *redeemCache
}

func (s *RedeemCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewRedeemCache(s.rdb).(*redeemCache)
}

func (s *RedeemCacheSuite) TestAcquireAndReleaseRedeemLock() {
	ok, err := s.cache.AcquireRedeemLock(s.ctx, "CODE", 10*time.Second)
	require.NoError(s.T(), err, "AcquireRedeemLock")
	require.True(s.T(), ok)

	// Second acquire should fail
	ok, err = s.cache.AcquireRedeemLock(s.ctx, "CODE", 10*time.Second)
	require.NoError(s.T(), err, "AcquireRedeemLock 2")
	require.False(s.T(), ok, "expected lock to be held")

	// Release
	require.NoError(s.T(), s.cache.ReleaseRedeemLock(s.ctx, "CODE"), "ReleaseRedeemLock")

	// Now acquire should succeed
	ok, err = s.cache.AcquireRedeemLock(s.ctx, "CODE", 10*time.Second)
	require.NoError(s.T(), err, "AcquireRedeemLock after release")
	require.True(s.T(), ok)
}

func (s *RedeemCacheSuite) TestAcquireRedeemLock_TTL() {
	lockKey := redeemLockKeyPrefix + "CODE2"
	lockTTL := 15 * time.Second

	ok, err := s.cache.AcquireRedeemLock(s.ctx, "CODE2", lockTTL)
	require.NoError(s.T(), err, "AcquireRedeemLock CODE2")
	require.True(s.T(), ok)

	ttl, err := s.rdb.TTL(s.ctx, lockKey).Result()
	require.NoError(s.T(), err, "TTL lock key")
	s.AssertTTLWithin(ttl, 1*time.Second, lockTTL)
}

func (s *RedeemCacheSuite) TestReleaseRedeemLock_Idempotent() {
	// Release a lock that doesn't exist should not error
	require.NoError(s.T(), s.cache.ReleaseRedeemLock(s.ctx, "NONEXISTENT"))

	// Acquire, release, release again
	ok, err := s.cache.AcquireRedeemLock(s.ctx, "IDEMPOTENT", 10*time.Second)
	require.NoError(s.T(), err)
	require.True(s.T(), ok)
	require.NoError(s.T(), s.cache.ReleaseRedeemLock(s.ctx, "IDEMPOTENT"))
	require.NoError(s.T(), s.cache.ReleaseRedeemLock(s.ctx, "IDEMPOTENT"), "second release should be idempotent")
}

func TestRedeemCacheSuite(t *testing.T) {
	suite.Run(t, new(RedeemCacheSuite))
}
