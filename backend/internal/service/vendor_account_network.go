package service

import (
	"context"
	"database/sql"
	"fmt"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"time"
)

// Network requests use a fixed egress snapshot and expiring reference. SQL locks
// protect the two short validation/commit phases, never an upstream round trip.
// Revocation or a proxy/token change in between discards the result.
func (s *VendorService) vendorAccountNetwork(ctx context.Context, v *Vendor, proxyID *int64, direct bool, account *Account, prepare func(*sql.Tx) error, request func(context.Context, string) error, commit func(*sql.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	lease, err := oauth.GenerateSessionID()
	if err != nil {
		return err
	}
	release, err := s.vendorNetworkSlot(ctx, v.ID, lease)
	if err != nil {
		return err
	}
	defer release()
	var proxy string
	var version int64
	err = s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		var e error
		proxy, version, e = s.vendorAccountEgress(ctx, tx, v, proxyID, direct)
		if e != nil {
			return e
		}
		if account != nil {
			if e = s.vendorLockAccount(ctx, tx, v, account.ID); e != nil {
				return e
			}
			if e = vendorAccountSnapshotMatches(ctx, tx, account); e != nil {
				return e
			}
		}
		if prepare != nil {
			if e = prepare(tx); e != nil {
				return e
			}
		}
		if proxyID != nil {
			return s.RegisterVendorProxyReference(ctx, v.ID, *proxyID, "inflight", lease, time.Now().Add(90*time.Second))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if proxyID != nil {
		defer func() {
			cleanup, c := context.WithTimeout(context.Background(), 3*time.Second)
			defer c()
			_ = s.ReleaseVendorProxyReference(cleanup, v.ID, *proxyID, "inflight", lease)
		}()
	}
	if err = request(ctx, proxy); err != nil {
		return err
	}
	return s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		_, current, e := s.vendorAccountEgress(ctx, tx, v, proxyID, direct)
		if e != nil {
			return e
		}
		if current != version {
			return ErrVendorAccountConflict
		}
		if account != nil {
			if e = s.vendorLockAccount(ctx, tx, v, account.ID); e != nil {
				return e
			}
			if e = vendorAccountSnapshotMatches(ctx, tx, account); e != nil {
				return e
			}
		}
		if commit != nil {
			return commit(tx)
		}
		return nil
	})
}

func vendorCredentialSnapshot(a *Account) *vendorVerifiedCredential {
	identity := a.GetCredential("chatgpt_account_id")
	if a.Platform == PlatformAnthropic {
		identity = a.GetCredential("account_uuid") + "\x00" + a.GetCredential("org_uuid")
	}
	return &vendorVerifiedCredential{Identity: identity, Credentials: a.Credentials, Method: a.GetExtraString("vendor_auth_method")}
}

// Expiring per-vendor slots bound provider traffic across all application nodes.
func (s *VendorService) vendorNetworkSlot(ctx context.Context, vendorID int64, lease string) (func(), error) {
	if s.rdb == nil {
		return nil, vendorAccountUnavailable()
	}
	key := fmt.Sprintf("vendor:account:network:%d", vendorID)
	allowed, err := s.rdb.Eval(ctx, "local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000);redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now);if redis.call('ZCARD',KEYS[1])>=2 then return 0 end;redis.call('ZADD',KEYS[1],now+90000,ARGV[1]);redis.call('PEXPIRE',KEYS[1],95000);return 1", []string{key}, lease).Int()
	if err != nil {
		return nil, vendorAccountUnavailable()
	}
	if allowed != 1 {
		return nil, infraerrors.New(429, "VENDOR_ACCOUNT_BUSY", "Two account operations are already running; retry shortly")
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.rdb.ZRem(cleanup, key, lease).Err()
	}, nil
}
