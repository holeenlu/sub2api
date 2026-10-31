package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/sessionstore"
	"github.com/stretchr/testify/require"
)

func TestOpenAIDirectOperationsRejectOutsideLifetime(t *testing.T) {
	s := NewOpenAIOAuthService(nil, nil)
	a := &Account{ID: 23, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	operations := []struct {
		name string
		call func() error
	}{
		{"PAT", func() error { _, err := s.ValidateCodexPersonalAccessToken(t.Context(), "at-fixture", ""); return err }},
		{"import", func() error { return s.ImportExcelCredentials(t.Context(), a, nil) }},
		{"token", func() error { _, err := s.ExcelAccessToken(t.Context(), a, nil); return err }},
		{"rejection", func() error { return s.rejectExcelAccessToken(t.Context(), a, "fixture", nil) }},
		{"status", func() error { _, err := s.ExcelAuthorizationStatus(t.Context(), a); return err }},
		{"delete", func() error { return s.DeleteExcelAuthorization(t.Context(), a) }},
		{"export", func() error { _, err := s.ExportAccountAuthorizations(t.Context(), a); return err }},
	}
	check := func(phase string) {
		for _, op := range operations {
			t.Run(phase+"/"+op.name, func(t *testing.T) { require.ErrorIs(t, op.call(), sessionstore.ErrClosed) })
		}
	}
	check("before_start")
	startSessionOwnerTest(t, s)
	drainCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	require.NoError(t, s.Drain(drainCtx))
	cancel()
	check("after_drain")
	stopSessionOwnerTest(t, s)
	check("after_stop")
}

func TestOpenAIDirectPATStopCancelsRealHTTPAndJoins(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
	}))
	t.Cleanup(server.Close)
	previousURL := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = server.URL
	t.Cleanup(func() { openAICodexPATWhoamiURL = previousURL })
	s := NewOpenAIOAuthService(nil, nil)
	startSessionOwnerTest(t, s)
	result := make(chan error, 1)
	go func() {
		_, err := s.ValidateCodexPersonalAccessToken(context.Background(), "at-fixture", "")
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("PAT request did not reach local server")
	}
	drainCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	require.ErrorIs(t, s.Drain(drainCtx), context.DeadlineExceeded)
	cancel()
	_, err := s.ValidateCodexPersonalAccessToken(t.Context(), "at-second", "")
	require.ErrorIs(t, err, sessionstore.ErrClosed)
	stopCtx, cancelStop := context.WithTimeout(t.Context(), time.Second)
	require.NoError(t, s.Stop(stopCtx))
	cancelStop()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("owner cancellation did not reach real HTTP request")
	}
	require.Equal(t, "OPENAI_CODEX_PAT_VALIDATE_FAILED", infraerrors.Reason(<-result))
}

type lifecycleBlockedExcelRepo struct {
	*excelGrantTestRepo
	entered, canceled, release chan struct{}
}

func (r *lifecycleBlockedExcelRepo) GetExcelCredentials(ctx context.Context, _ int64) (string, error) {
	close(r.entered)
	<-ctx.Done()
	close(r.canceled)
	<-r.release
	return "", ctx.Err()
}
func (r *lifecycleBlockedExcelRepo) AccountAuthorizationSnapshot(ctx context.Context, a *Account) (*AccountAuthorizationCiphertexts, error) {
	_, err := r.GetExcelCredentials(ctx, a.ID)
	return nil, err
}
func (r *lifecycleBlockedExcelRepo) CreateWithAccountAuthorizations(context.Context, *Account, []AccountGroup, *AccountAuthorizationCiphertexts) error {
	return errors.New("unexpected create during lifecycle test")
}

func TestOpenAIDirectExcelStopOwnsEveryRepositoryOperation(t *testing.T) {
	for _, name := range []string{"import", "token", "rejection", "status", "delete", "export"} {
		t.Run(name, func(t *testing.T) {
			s, original, a := excelGrantService(t)
			repo := &lifecycleBlockedExcelRepo{original, make(chan struct{}), make(chan struct{}), make(chan struct{})}
			s.excelAccountRepo = repo
			var once sync.Once
			release := func() { once.Do(func() { close(repo.release) }) }
			t.Cleanup(release)
			grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "fixture")
			result := make(chan error, 1)
			go func() {
				var err error
				switch name {
				case "import":
					err = s.ImportExcelCredentials(context.Background(), a, grant)
				case "token":
					_, err = s.ExcelAccessToken(context.Background(), a, nil)
				case "rejection":
					err = s.rejectExcelAccessToken(context.Background(), a, "fixture", nil)
				case "status":
					_, err = s.ExcelAuthorizationStatus(context.Background(), a)
				case "delete":
					err = s.DeleteExcelAuthorization(context.Background(), a)
				case "export":
					_, err = s.ExportAccountAuthorizations(context.Background(), a)
				}
				result <- err
			}()
			select {
			case <-repo.entered:
			case <-time.After(time.Second):
				t.Fatal("operation did not reach repository")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			require.ErrorIs(t, s.Stop(ctx), context.DeadlineExceeded, "canceled dependency has not returned")
			cancel()
			select {
			case <-repo.canceled:
			case <-time.After(time.Second):
				t.Fatal("repository context was not canceled")
			}
			select {
			case <-result:
				t.Fatal("dependency falsely joined")
			default:
			}
			release()
			stopSessionOwnerTest(t, s)
			require.Error(t, <-result)
			require.Empty(t, original.ciphertext, "failed operation must retain credentials")
		})
	}
}

type lifecycleExcelPersistRepo struct {
	*excelGrantTestRepo
	entered chan context.Context
	release chan struct{}
}

func (r *lifecycleExcelPersistRepo) StoreExcelCredentials(ctx context.Context, account *Account, previous, next string) (bool, error) {
	r.entered <- ctx
	select {
	case <-r.release:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return r.excelGrantTestRepo.StoreExcelCredentials(ctx, account, previous, next)
}

func TestOpenAIDirectExcelStopJoinsRotatedCredentialPersistence(t *testing.T) {
	s, original, a := excelGrantService(t)
	old := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Minute), "old")
	next := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "new")
	require.NoError(t, s.ImportExcelCredentials(t.Context(), a, old))
	repo := &lifecycleExcelPersistRepo{original, make(chan context.Context, 1), make(chan struct{})}
	s.excelAccountRepo = repo
	var once sync.Once
	release := func() { once.Do(func() { close(repo.release) }) }
	t.Cleanup(release)
	s.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) {
		return &openai.TokenResponse{AccessToken: next["access_token"].(string), RefreshToken: next["refresh_token"].(string), IDToken: next["id_token"].(string)}, nil
	}}
	result := make(chan error, 1)
	go func() {
		token, err := s.ExcelAccessToken(context.Background(), a, nil)
		if err == nil && token != next["access_token"] {
			err = errors.New("fresh token lost")
		}
		result <- err
	}()
	var persistCtx context.Context
	select {
	case persistCtx = <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("rotation did not reach persistence")
	}
	deadline, bounded := persistCtx.Deadline()
	require.True(t, bounded)
	require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	require.ErrorIs(t, s.Stop(ctx), context.DeadlineExceeded)
	cancel()
	require.NoError(t, persistCtx.Err(), "Stop must not discard an already rotated refresh token")
	release()
	stopSessionOwnerTest(t, s)
	require.NoError(t, <-result)
	_, saved, err := s.readExcelGrant(t.Context(), a)
	require.NoError(t, err)
	require.Equal(t, next["refresh_token"], saved["refresh_token"])
}
