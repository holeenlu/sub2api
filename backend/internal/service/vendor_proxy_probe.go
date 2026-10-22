package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"time"
)

type VendorProxyProbeResult struct {
	Status        string                   `json:"status"`
	ConfigVersion int64                    `json:"config_version"`
	Test          *ProxyTestResult         `json:"test,omitempty"`
	Quality       *ProxyQualityCheckResult `json:"quality,omitempty"`
	CollectedAt   time.Time                `json:"collected_at"`
}

func sanitizeVendorProxyQuality(in *ProxyQualityCheckResult) *ProxyQualityCheckResult {
	if in == nil {
		return nil
	}
	out := *in
	out.Items = append([]ProxyQualityCheckItem(nil), in.Items...)
	for i := range out.Items {
		v := &out.Items[i]
		v.CFRay = ""
		switch v.Status {
		case "pass":
			v.Message = "Target reachable"
		case "warn":
			v.Message = "Target rate limited"
		case "challenge":
			v.Message = "Target requires verification"
		default:
			v.Message = "Target connection failed"
		}
	}
	return &out
}

func (s *VendorService) ProbeVendorProxy(ctx context.Context, id, version int64, quality bool) (*VendorProxyProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	vID, err := s.proxyScope(ctx)
	if err != nil {
		return nil, err
	}
	if version <= 0 {
		return nil, ErrVendorProxyVersion
	}
	if s.admin == nil {
		return nil, infraerrors.ServiceUnavailable("PROXY_PROBER_UNAVAILABLE", "probe service unavailable")
	}
	tx, err := s.beginProxyMutation(ctx, vID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := ValidateVendorProxyBinding(ctx, tx, vID, id)
	if err != nil {
		if infraerrors.Reason(err) == "PROXY_UNAVAILABLE" {
			return &VendorProxyProbeResult{Status: "skipped", ConfigVersion: version, CollectedAt: time.Now()}, nil
		}
		return nil, err
	}
	if p.ConfigVersion != version {
		return nil, ErrVendorProxyVersion
	}
	if s.rdb == nil {
		return nil, infraerrors.ServiceUnavailable("PROXY_PROBER_UNAVAILABLE", "probe limiter unavailable")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	lease := hex.EncodeToString(nonce[:])
	now := time.Now()
	// A shared rolling minute rate limit and expiring active leases. A crashed
	// worker cannot permanently consume concurrency. No queued backend jobs.
	key := fmt.Sprintf("vendor:proxy:probe:{%d}", vID)
	n, err := s.rdb.Eval(ctx, `redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ARGV[1]); redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',ARGV[2]); if redis.call('ZCARD',KEYS[1])>=2 or redis.call('ZCARD',KEYS[2])>=20 then return 0 end; redis.call('ZADD',KEYS[1],ARGV[3],ARGV[4]); redis.call('ZADD',KEYS[2],ARGV[5],ARGV[4]); redis.call('PEXPIRE',KEYS[1],120000); redis.call('PEXPIRE',KEYS[2],120000); return 1`, []string{key + ":active", key + ":rate"}, now.UnixMilli(), now.Add(-time.Minute).UnixMilli(), now.Add(2*time.Minute).UnixMilli(), lease, now.UnixMilli()).Int()
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("PROXY_PROBER_UNAVAILABLE", "probe limiter unavailable")
	}
	if n != 1 {
		return nil, infraerrors.New(429, "PROXY_PROBE_LIMIT", "proxy test rate or concurrency limit reached")
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		s.rdb.ZRem(cleanup, key+":active", lease)
		_ = s.ReleaseVendorProxyReference(cleanup, vID, id, "probe", lease)
	}()
	if err = s.RegisterVendorProxyReference(ctx, vID, id, "probe", lease, now.Add(2*time.Minute)); err != nil {
		return nil, err
	}
	if err = commitVendorProxyMutation(ctx, tx); err != nil {
		return nil, err
	}
	out := &VendorProxyProbeResult{ConfigVersion: version, CollectedAt: time.Now()}
	parent := ctx
	ctx = authz.WithRevalidation(ctx, func(context.Context) error { _, e := s.proxyScope(parent); return e })
	ctx = context.WithValue(ctx, expectedProxyProbeVersionKey{}, version)
	if quality {
		out.Quality, err = s.admin.CheckProxyQuality(ctx, id)
		out.Quality = sanitizeVendorProxyQuality(out.Quality)
		out.Status = "success"
		if out.Quality != nil && out.Quality.FailedCount > 0 {
			out.Status = "failed"
		}
	} else {
		out.Test, err = s.admin.TestProxy(ctx, id)
		out.Status = "failed"
		if out.Test != nil {
			out.Test.Message = "Proxy connection failed"
			if out.Test.Success {
				out.Test.Message = "Proxy is accessible"
				out.Status = "success"
			}
		}
	}
	if err != nil {
		if infraerrors.Reason(err) == "PROXY_VERSION_CONFLICT" || infraerrors.Reason(err) == "PROXY_NOT_FOUND" {
			return nil, err
		}
		return nil, infraerrors.New(502, "PROXY_PROBE_FAILED", "proxy test failed")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if _, err = s.proxyScope(ctx); err != nil {
		return nil, err
	}
	current, err := s.ownedProxy(ctx, id, vID)
	if err != nil {
		return nil, err
	}
	if current.ConfigVersion != version {
		return nil, ErrVendorProxyVersion
	}
	out.CollectedAt = time.Now()
	action := "proxy.test"
	if quality {
		action = "proxy.quality_check"
	}
	s.VendorAudit(ctx, vID, action, id, out.Status)
	return out, nil
}
