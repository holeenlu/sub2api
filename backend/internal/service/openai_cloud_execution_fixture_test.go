package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type cloudForwardKeys struct {
	APIKeyRepository
	key *APIKey
}

func (r *cloudForwardKeys) GetByID(_ context.Context, id int64) (*APIKey, error) {
	if r.key == nil || r.key.ID != id {
		return nil, ErrAPIKeyNotFound
	}
	copy := *r.key
	return &copy, nil
}

type cloudForwardAccounts struct{ *cloudAuthorizationRepoFixture }

func (r *cloudForwardAccounts) ValidateVendorSupply(_ context.Context, account *Account, group *int64) error {
	if account == nil || r.account == nil || account.ID != r.account.ID || account.VendorID != nil || account.IsShadow() {
		return ErrVendorSupplyUnavailable
	}
	if group != nil {
		for _, id := range r.account.GroupIDs {
			if id == *group {
				return nil
			}
		}
		return ErrGroupNotAllowed
	}
	return nil
}
func (r *cloudForwardAccounts) ValidateVendorAuthorizationMode(ctx context.Context, account *Account, group *int64, mode string) error {
	if mode != VendorOpenAIModeNative {
		return ErrVendorSupplyUnavailable
	}
	return r.ValidateVendorSupply(ctx, account, group)
}

// These fixtures enter below the public handler, so they explicitly construct
// the same authenticated root and actual routing receipts the handler supplies.
// No production authorization or capacity port is replaced by an allow callback.
func cloudFixtureExecutionContext(t *testing.T, svc *OpenAIGatewayService, in *OpenAIRecordUsageInput, parent context.Context, body []byte, originalModel, transport string) (context.Context, func()) {
	t.Helper()
	if originalModel == "" {
		originalModel = gjson.GetBytes(body, "model").String()
	}
	keys := svc.cloudExecutionKeys()
	require.NotNil(t, keys)
	// Every independent client submission has its own operation/budget; a
	// context inherited from the previous WS response must not revive its core.
	ctx := context.WithValue(parent, nativeHTTPOperationKey{}, (*nativeHTTPOperation)(nil))
	ctx = context.WithValue(ctx, nativeDispatchOperationKey{}, (*nativeDispatchOperation)(nil))
	ctx = WithOpenAITurnBudget(ctx, NewOpenAITurnBudget(svc.cfg))
	request := NativeDispatchRequest{OperationID: uuid.NewString(), UserID: in.User.ID, APIKeyID: in.APIKey.ID, APIKeyDigest: sha256.Sum256([]byte(in.APIKey.Key)), RequestedModel: originalModel, ClientIP: "192.0.2.8", Transport: transport}
	if in.APIKey.GroupID != nil {
		request.GroupBound = true
		request.GroupID = *in.APIKey.GroupID
	}
	var err error
	if transport == "ws" {
		ctx, err = svc.BindNativeWSDispatch(ctx, keys, request)
	} else {
		ctx, err = svc.BindNativeHTTPDispatch(ctx, keys, request)
	}
	require.NoError(t, err)
	capacity, ok := svc.concurrencyService.cache.(routing.CapacityStore)
	require.True(t, ok)
	var releases []func()
	for _, r := range []routing.CapacityRequest{{ID: in.User.ID, User: true, Limit: in.User.Concurrency, Token: uuid.NewString()}, {ID: in.Account.ID, Limit: nativeSchedulingConcurrency(ctx, in.Account, svc.settingService), Token: uuid.NewString()}} {
		acquired, err := routing.AcquireCapacity(ctx, capacity, r, nil)
		require.NoError(t, err)
		require.True(t, acquired.Acquired)
		releases = append(releases, acquired.ReleaseFunc)
	}
	if transport == "ws" {
		ctx, err = svc.BindNativeWSOperation(ctx, body)
	} else {
		ctx, err = svc.BindNativeHTTPOperation(ctx, model.ProtocolResponses, sha256.Sum256(body))
		if err == nil {
			err = nativeHTTPFromContext(ctx).core.Begin(ctx)
		}
	}
	require.NoError(t, err)
	var once sync.Once
	finish := func() {
		once.Do(func() {
			cause := context.Cause(ctx)
			if cause == nil {
				cause = errors.New("isolated ingress fixture released without another candidate")
			}
			// Imported terminal states remain unchanged. Cleanup cannot turn an
			// unstarted/recoverable request into a fabricated successful result.
			_ = nativeHTTPFromContext(ctx).core.Finish(ctx, cause)
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
		})
	}
	t.Cleanup(finish)
	return ctx, finish
}

func cloudFixtureForward(t *testing.T, svc *OpenAIGatewayService, in *OpenAIRecordUsageInput, ctx context.Context, c *gin.Context, body []byte) (*OpenAIForwardResult, error) {
	ctx, finish := cloudFixtureExecutionContext(t, svc, in, ctx, body, "", "http")
	defer finish()
	c.Request = c.Request.WithContext(ctx)
	return TrackNativeHTTPForward(ctx, func() (*OpenAIForwardResult, error) { return svc.Forward(ctx, c, in.Account, body) })
}
func cloudFixtureForwardMode(t *testing.T, svc *OpenAIGatewayService, in *OpenAIRecordUsageInput, ctx context.Context, c *gin.Context, body []byte) (*OpenAIForwardResult, error) {
	ctx, finish := cloudFixtureExecutionContext(t, svc, in, ctx, body, "", "http")
	defer finish()
	c.Request = c.Request.WithContext(ctx)
	return TrackNativeHTTPForward(ctx, func() (*OpenAIForwardResult, error) { return svc.forwardOpenAICloud(ctx, c, in.Account, body) })
}

func cloudFixtureWSHooks(t *testing.T, svc *OpenAIGatewayService, in *OpenAIRecordUsageInput) *OpenAIWSIngressHooks {
	var mu sync.Mutex
	releases := make(map[int]func())
	return &OpenAIWSIngressHooks{PrepareCloudTurn: func(ctx context.Context, turn int, body []byte, original string) (context.Context, error) {
		bound, finish := cloudFixtureExecutionContext(t, svc, in, ctx, body, original, "ws")
		mu.Lock()
		releases[turn] = finish
		mu.Unlock()
		return svc.CaptureCloudBillingSnapshot(bound, in)
	}, ReleaseTurn: func(turn int) {
		mu.Lock()
		finish := releases[turn]
		delete(releases, turn)
		mu.Unlock()
		if finish != nil {
			finish()
		}
	}}
}

func cloudFixtureBindDirect(t *testing.T, svc *OpenAIGatewayService, in *OpenAIRecordUsageInput, ctx context.Context, c *gin.Context, body []byte, originalModel, transport string) context.Context {
	bound, _ := cloudFixtureExecutionContext(t, svc, in, ctx, body, originalModel, transport)
	if c != nil {
		c.Request = c.Request.WithContext(bound)
	}
	return bound
}

func cloudFixtureSessionsSource(t *testing.T, svc *OpenAIGatewayService, in *OpenAIRecordUsageInput, ctx context.Context, body []byte) (context.Context, *cloudResponseSource) {
	bound, _ := cloudFixtureExecutionContext(t, svc, in, ctx, body, "", "http")
	source, err := svc.bindResponseCloud(bound)
	require.NoError(t, err)
	return bound, source
}
