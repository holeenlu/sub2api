package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// VendorSettlementAllocation preserves supplier accounting independently of raw usage retention.
type VendorSettlementAllocation struct{ ent.Schema }

func (VendorSettlementAllocation) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "vendor_settlement_allocations"}}
}
func (VendorSettlementAllocation) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("payer_vendor_id").Optional().Nillable().Immutable(),
		field.Int64("statement_id"),
		field.Int64("line_id"),
		field.Int64("usage_log_id"),
		field.String("source_fingerprint").MaxLen(64).Optional().Nillable().Immutable(),
		field.Int64("vendor_id"),
		field.Bool("active").Default(true),
		field.JSON("snapshot", map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}
func (VendorSettlementAllocation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("usage_log_id").Unique().StorageKey("vendor_allocations_platform_usage_idx").Annotations(entsql.IndexWhere("active AND payer_vendor_id IS NULL")),
		index.Fields("source_fingerprint").Unique().StorageKey("vendor_allocations_platform_source_idx").Annotations(entsql.IndexWhere("active AND payer_vendor_id IS NULL AND source_fingerprint IS NOT NULL")),
		index.Fields("payer_vendor_id", "usage_log_id").Unique().StorageKey("vendor_allocations_supplier_usage_idx").Annotations(entsql.IndexWhere("active AND payer_vendor_id IS NOT NULL")),
		index.Fields("payer_vendor_id", "source_fingerprint").Unique().StorageKey("vendor_allocations_supplier_source_idx").Annotations(entsql.IndexWhere("active AND payer_vendor_id IS NOT NULL AND source_fingerprint IS NOT NULL")),
		index.Fields("statement_id", "usage_log_id").Unique(),
	}
}
