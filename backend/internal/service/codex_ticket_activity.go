package service

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

type codexTicketActivity struct {
	sync.Mutex
	active   int
	harvests map[*byte]context.CancelFunc
}

func (s *OpenAIGatewayService) codexTicketActivity(accountID int64) *codexTicketActivity {
	value, _ := s.openaiCodexTicketActivity.LoadOrStore(accountID, &codexTicketActivity{})
	activity, ok := value.(*codexTicketActivity)
	if !ok {
		panic("invalid codex ticket activity state")
	}
	return activity
}

// Business traffic wins: cancel probes for this account, including other models.
// Streaming holds the activity until EOF/Close; idle WS turns release it.
func (s *OpenAIGatewayService) beginCodexTicketBusiness(account *Account) func() {
	if !isOpenAICodexTicketAccount(account) || !s.openAICodexTicketEnabled() {
		return func() {}
	}
	a := s.codexTicketActivity(account.ID)
	a.Lock()
	a.active++
	for _, cancel := range a.harvests {
		cancel()
	}
	a.Unlock()
	return sync.OnceFunc(func() { a.Lock(); a.active--; a.Unlock() })
}

func (s *OpenAIGatewayService) codexTicketAccountBusy(ctx context.Context, accountID int64) bool {
	a := s.codexTicketActivity(accountID)
	a.Lock()
	busy := a.active > 0
	a.Unlock()
	if busy || s.concurrencyService == nil {
		return busy
	}
	query, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	counts, err := s.concurrencyService.GetAccountConcurrencyBatch(query, []int64{accountID})
	// A failed load read pauses background probes, never business traffic.
	return err != nil || counts[accountID] > 0
}

func (s *OpenAIGatewayService) beginCodexTicketHarvest(ctx context.Context, accountID int64) (context.Context, func(), error) {
	if s.codexTicketAccountBusy(ctx, accountID) {
		return ctx, nil, ErrCodexTicketBusy
	}
	probe, cancel := context.WithCancel(ctx)
	a := s.codexTicketActivity(accountID)
	// Nonzero-sized token: distinct concurrent harvests need distinct map keys.
	key := new(byte)
	a.Lock()
	if a.active > 0 {
		a.Unlock()
		cancel()
		return ctx, nil, ErrCodexTicketBusy
	}
	if a.harvests == nil {
		a.harvests = make(map[*byte]context.CancelFunc)
	}
	a.harvests[key] = cancel
	a.Unlock()
	return probe, func() { cancel(); a.Lock(); delete(a.harvests, key); a.Unlock() }, nil
}

type codexTicketActivityBody struct {
	io.ReadCloser
	release func()
}

func (b *codexTicketActivityBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.release()
	}
	return n, err
}
func (b *codexTicketActivityBody) Close() error { defer b.release(); return b.ReadCloser.Close() }
func attachCodexTicketActivity(response *http.Response, err error, release func()) (*http.Response, error) {
	if err != nil || response == nil || response.Body == nil {
		release()
	} else {
		response.Body = &codexTicketActivityBody{ReadCloser: response.Body, release: release}
	}
	return response, err
}

// Passthrough reads and completes turns on separate goroutines.
type codexTicketTurnActivity struct {
	sync.Mutex
	release func()
}

func (a *codexTicketTurnActivity) begin(s *OpenAIGatewayService, account *Account) {
	a.Lock()
	defer a.Unlock()
	if a.release == nil {
		a.release = s.beginCodexTicketBusiness(account)
	}
}
func (a *codexTicketTurnActivity) end() {
	a.Lock()
	defer a.Unlock()
	if a.release != nil {
		a.release()
		a.release = nil
	}
}
