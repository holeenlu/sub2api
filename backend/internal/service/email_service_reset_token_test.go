//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type resetTokenCacheStub struct {
	emailCacheStub
	stored       *PasswordResetTokenData
	consumedHash string
	consumeErr   error
	getErr       error
}

func (s *resetTokenCacheStub) GetPasswordResetToken(context.Context, string) (*PasswordResetTokenData, error) {
	return s.stored, s.getErr
}

func (s *resetTokenCacheStub) ConsumePasswordResetToken(_ context.Context, _ string, tokenHash string) (bool, error) {
	s.consumedHash = tokenHash
	if s.consumeErr != nil {
		return false, s.consumeErr
	}
	if s.stored == nil || s.stored.Token != tokenHash {
		return false, nil
	}
	s.stored = nil
	return true, nil
}

func TestConsumePasswordResetToken_ComparesHashNotPlaintext(t *testing.T) {
	token := "deadbeef"
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	require.Equal(t, hash, hashPasswordResetToken(token))
	require.NotEqual(t, token, hashPasswordResetToken(token))

	cache := &resetTokenCacheStub{stored: &PasswordResetTokenData{Token: hash}}
	svc := NewEmailService(nil, cache)
	require.ErrorIs(t, svc.ConsumePasswordResetToken(context.Background(), "a@b.c", "wrong"), ErrInvalidResetToken)
	require.NotNil(t, cache.stored)

	require.NoError(t, svc.ConsumePasswordResetToken(context.Background(), "a@b.c", token))
	require.Equal(t, hash, cache.consumedHash)
	require.ErrorIs(t, svc.ConsumePasswordResetToken(context.Background(), "a@b.c", token), ErrInvalidResetToken)

	// A legacy plaintext value (issued before upgrade) no longer validates.
	cache.stored = &PasswordResetTokenData{Token: token}
	require.ErrorIs(t, svc.ConsumePasswordResetToken(context.Background(), "a@b.c", token), ErrInvalidResetToken)
}

func TestConsumePasswordResetToken_CacheFailureIsUnavailable(t *testing.T) {
	cache := &resetTokenCacheStub{stored: &PasswordResetTokenData{Token: hashPasswordResetToken("token")}, consumeErr: errors.New("cache unavailable")}
	svc := NewEmailService(nil, cache)
	require.ErrorIs(t, svc.ConsumePasswordResetToken(context.Background(), "a@b.c", "token"), ErrServiceUnavailable)
	require.NotNil(t, cache.stored)
	cache.consumeErr = nil
	cache.getErr = errors.New("cache read unavailable")
	require.ErrorIs(t, svc.ConsumePasswordResetToken(context.Background(), "a@b.c", "token"), ErrServiceUnavailable)
	cache.getErr = redis.Nil
	require.ErrorIs(t, svc.ConsumePasswordResetToken(context.Background(), "a@b.c", "token"), ErrInvalidResetToken)
}
