package service

import (
	"context"
	"database/sql"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
)

// Native account edits must share the supplier transaction: otherwise notes,
// bindings and scheduling parameters could partially commit or race archiving.
// This driver lends the existing SQL transaction to Ent; the caller alone owns
// commit/rollback. Native repository writes retain their outbox/identity guards.
type vendorAccountNativeDriver struct {
	*entsql.Driver
	tx *sql.Tx
}

func (d *vendorAccountNativeDriver) Tx(context.Context) (dialect.Tx, error) {
	return &entsql.Tx{Conn: d.Conn, Tx: d.tx}, nil
}
func (s *VendorService) updateVendorAccountScheduling(ctx context.Context, tx *sql.Tx, id int64, in VendorAccountUpdate) error {
	if s.admin == nil {
		return vendorAccountUnavailable()
	}
	driver := &vendorAccountNativeDriver{Driver: entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx}), tx: tx}
	native, err := dbent.NewClient(dbent.Driver(driver)).Tx(ctx)
	if err != nil {
		return err
	}
	_, err = s.admin.UpdateAccount(dbent.NewTxContext(ctx, native), id, &UpdateAccountInput{Concurrency: in.Concurrency, Priority: in.Priority})
	return err
}
