package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestEasyPaySecurityCheckoutSignatureAmbiguity(t *testing.T) {
	const key = "dummy-secret"
	checkout := map[string]string{"pid": "merchant", "money": "10.00", "out_trade_no": "order", "name": "Recharge", "notify_url": "https://site/notify", "return_url": "https://site/payment/result?status=success&trade_no=fake&trade_status=TRADE_SUCCESS", "type": "alipay"}
	forged := map[string]string{}
	for k, v := range checkout {
		forged[k] = v
	}
	forged["return_url"] = "https://site/payment/result?status=success"
	forged["trade_no"] = "fake"
	forged["trade_status"] = "TRADE_SUCCESS"
	// This equality is the actual protocol flaw, NOT an MD5 collision. Keep the
	// protocol unchanged and prove its cross-purpose token no longer grants credit.
	require.Equal(t, easyPaySign(checkout, key), easyPaySign(forged, key))
	forged["sign"] = easyPaySign(checkout, key)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "order", r.Form.Get("out_trade_no"))
		require.Equal(t, key, r.Form.Get("key"))
		_, _ = w.Write([]byte(`{"code":1,"status":0,"money":"10.00","trade_no":"fake"}`))
	}))
	defer server.Close()
	e := &EasyPay{config: map[string]string{"pid": "merchant", "pkey": key, "apiBase": server.URL}, httpClient: server.Client()}
	_, err := e.VerifyNotification(context.Background(), encodeEasyPaySecurityParams(forged), nil)
	require.ErrorContains(t, err, "settlement")
}

func encodeEasyPaySecurityParams(params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	return q.Encode()
}

func TestEasyPaySecurityLegitimateAndAlternateNotifications(t *testing.T) {
	cases := []struct {
		name, body string
		wantErr    bool
	}{
		{"legacy paid", `{"code":1,"status":1,"money":"10.00","trade_no":"paid"}`, false},
		{"nested paid", `{"code":1,"data":{"pid":"merchant","out_trade_no":"order","trade_status":"TRADE_SUCCESS","money":"10.00","trade_no":"paid"}}`, false},
		{"wrong merchant", `{"code":1,"pid":"other","status":1,"money":"10.00","trade_no":"paid"}`, true},
		{"wrong order", `{"code":1,"out_trade_no":"other","status":1,"money":"10.00","trade_no":"paid"}`, true},
		{"wrong trade", `{"code":1,"status":1,"money":"10.00","trade_no":"other"}`, true},
		{"wrong amount", `{"code":1,"status":1,"money":"11.00","trade_no":"paid"}`, true},
		{"error with paid fields", `{"code":0,"status":1,"money":"10.00","trade_no":"paid"}`, true},
		{"missing transaction", `{"code":1,"status":1,"money":"10.00"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			e := &EasyPay{config: map[string]string{"pid": "merchant", "pkey": "dummy", "apiBase": server.URL}, httpClient: server.Client()}
			params := map[string]string{"pid": "merchant", "out_trade_no": "order", "trade_no": "paid", "trade_status": "TRADE_SUCCESS", "money": "10.00", "name": "product with & and ="}
			params["sign"] = easyPaySign(params, "dummy")
			n, err := e.VerifyNotification(context.Background(), encodeEasyPaySecurityParams(params), nil)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, payment.ProviderStatusSuccess, n.Status)
			}
			_, err = e.VerifyNotification(context.Background(), encodeEasyPaySecurityParams(params)+"&money=10.00", nil)
			require.ErrorContains(t, err, "duplicate")
		})
	}
}

func TestEasyPaySecurityNumericMerchantIDCompatibility(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1,"pid":1001,"status":1,"money":"10.00","trade_no":"paid","out_trade_no":"order"}`))
	}))
	defer server.Close()
	e := &EasyPay{config: map[string]string{"pid": "1001", "pkey": "dummy", "apiBase": server.URL}, httpClient: server.Client()}
	params := map[string]string{"pid": "1001", "out_trade_no": "order", "trade_no": "paid", "trade_status": "TRADE_SUCCESS", "money": "10.00"}
	params["sign"] = easyPaySign(params, "dummy")
	_, err := e.VerifyNotification(context.Background(), encodeEasyPaySecurityParams(params), nil)
	require.NoError(t, err)
}
