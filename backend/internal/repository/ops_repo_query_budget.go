package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

const (
	// 聚合 SQL 会同时消耗数据库 CPU 与连接；最多四条，池容量允许时给业务预留两个连接。
	// 小池最低一条观测通路；未配置池上限时也只允许一条，不推测可用容量。
	opsQueryMaxConcurrency = 4
	opsQueryDefaultTimeout = 30 * time.Second
)

// opsQueryBudget 由唯一的 Ops 仓储持有，零值与构造器路径均在首次查询时生效。
// 名额对应实际 SQL 连接，不对应可能继续调用其他查询的 Dashboard 方法。
type opsQueryBudget struct {
	mu      sync.Mutex
	active  int
	changed chan struct{}
}

func (b *opsQueryBudget) acquire(ctx context.Context, db *sql.DB) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		limit := 1
		if maxOpen := db.Stats().MaxOpenConnections; maxOpen > 0 {
			limit = min(opsQueryMaxConcurrency, max(1, maxOpen-2))
		}
		b.mu.Lock()
		if b.active < limit {
			b.active++
			b.mu.Unlock()
			return nil
		}
		if b.changed == nil {
			b.changed = make(chan struct{})
		}
		changed := b.changed
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (b *opsQueryBudget) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active--
	if b.changed != nil {
		close(b.changed)
		b.changed = nil
	}
}

type opsQueryLease struct {
	conn   *sql.Conn
	ctx    context.Context
	cancel context.CancelFunc
	budget *opsQueryBudget
	once   sync.Once
}

func (r *opsRepository) acquireOpsQuery(ctx context.Context) (*opsQueryLease, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil ops repository")
	}
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		ctx, cancel = context.WithTimeout(ctx, opsQueryDefaultTimeout)
	}
	if err := r.queryBudget.acquire(ctx, r.db); err != nil {
		cancel()
		return nil, err
	}
	conn, err := r.db.Conn(ctx)
	if err != nil {
		r.queryBudget.release()
		cancel()
		return nil, err
	}
	return &opsQueryLease{conn: conn, ctx: ctx, cancel: cancel, budget: &r.queryBudget}, nil
}

func (l *opsQueryLease) close() {
	l.once.Do(func() {
		// Conn.Close 等待正在使用连接的 rows/事务结束；名额不能先于回滚及连接归还释放。
		_ = l.conn.Close()
		l.cancel()
		l.budget.release()
	})
}

type opsQueryRows struct {
	*sql.Rows
	lease *opsQueryLease
}

func (r *opsRepository) queryOpsRows(ctx context.Context, query string, args ...any) (*opsQueryRows, error) {
	lease, err := r.acquireOpsQuery(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := lease.conn.QueryContext(lease.ctx, query, args...)
	if err != nil {
		err = opsQueryContextError(lease.ctx, err)
		lease.close()
		return nil, err
	}
	return &opsQueryRows{Rows: rows, lease: lease}, nil
}

func (r *opsQueryRows) Next() bool {
	if r.Rows.Next() {
		return true
	}
	// EOF 已消费完本条 SQL；先归还名额，调用方随后才可安全执行下钻或降级查询。
	_ = r.Close()
	return false
}

func (r *opsQueryRows) Close() error {
	err := r.Rows.Close()
	r.lease.close()
	return err
}

type opsQueryRow struct {
	row   *sql.Row
	lease *opsQueryLease
	err   error
}

func (r *opsRepository) queryOpsRow(ctx context.Context, query string, args ...any) *opsQueryRow {
	lease, err := r.acquireOpsQuery(ctx)
	if err != nil {
		return &opsQueryRow{err: err}
	}
	return &opsQueryRow{row: lease.conn.QueryRowContext(lease.ctx, query, args...), lease: lease}
}

func (r *opsQueryRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	defer r.lease.close()
	return opsQueryContextError(r.lease.ctx, r.row.Scan(dest...))
}

func (r *opsRepository) execOpsQuery(ctx context.Context, query string, args ...any) (sql.Result, error) {
	lease, err := r.acquireOpsQuery(ctx)
	if err != nil {
		return nil, err
	}
	defer lease.close()
	result, err := lease.conn.ExecContext(lease.ctx, query, args...)
	return result, opsQueryContextError(lease.ctx, err)
}

func opsQueryContextError(ctx context.Context, err error) error {
	// lib/pq 的取消错误不包装 context，保留 Dashboard 原有的超时降级与取消语义。
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
