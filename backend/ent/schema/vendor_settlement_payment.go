package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// VendorSettlementPayment preserves supplier accounting independently of raw usage retention.
type VendorSettlementPayment struct{ ent.Schema }

func (VendorSettlementPayment) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "vendor_settlement_payments"}}
}
func (VendorSettlementPayment) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("payer_vendor_id").Optional().Nillable().Immutable(),
		field.Int64("statement_id"),
		field.String("amount").SchemaType(map[string]string{dialect.Postgres: "numeric(30,2)"}),
		field.String("currency"),
		field.Time("paid_at"),
		field.String("channel"),
		field.String("reference"),
		field.Int64("actor_id"),
		field.String("idempotency_key"),
		field.String("input_hash"),
		field.Int64("reversal_of_id").Optional().Nillable(),
		field.String("reason").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("created_at").Default(time.Now),
	}
}
func (VendorSettlementPayment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("statement_id", "idempotency_key").Unique(),
		index.Fields("channel", "reference").Unique().StorageKey("vendor_payments_platform_receipt_idx").Annotations(entsql.IndexWhere("reversal_of_id IS NULL AND payer_vendor_id IS NULL")),
		index.Fields("payer_vendor_id", "channel", "reference").Unique().StorageKey("vendor_payments_supplier_receipt_idx").Annotations(entsql.IndexWhere("reversal_of_id IS NULL AND payer_vendor_id IS NOT NULL")),
	}
}
