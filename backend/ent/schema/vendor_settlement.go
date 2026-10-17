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

// VendorSettlement preserves supplier accounting independently of raw usage retention.
type VendorSettlement struct{ ent.Schema }

func (VendorSettlement) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "vendor_settlements"}}
}
func (VendorSettlement) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("payer_vendor_id").Optional().Nillable().Immutable(),
		field.String("number"),
		field.Int64("vendor_id"),
		field.String("kind"),
		field.Int64("parent_statement_id").Optional().Nillable(),
		field.String("status"),
		field.Int64("version"),
		field.Time("start_at"),
		field.Time("end_at"),
		field.String("timezone").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("snapshot_at"),
		field.Int64("watermark"),
		field.Int64("contract_version"),
		field.Int64("policy_version"),
		field.JSON("snapshot", map[string]any{}),
		field.String("snapshot_hash"),
		field.String("total").SchemaType(map[string]string{dialect.Postgres: "numeric(30,2)"}),
		field.String("paid").SchemaType(map[string]string{dialect.Postgres: "numeric(30,2)"}),
		field.String("idempotency_key"),
		field.String("input_hash"),
		field.Int64("created_by"),
		field.Int64("confirmed_by").Optional().Nillable(),
		field.Time("confirmed_at").Optional().Nillable(),
		field.String("void_reason").SchemaType(map[string]string{dialect.Postgres: "text"}).Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now),
	}
}
func (VendorSettlement) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("number").Unique(),
		index.Fields("vendor_id", "idempotency_key").Unique().StorageKey("vendor_settlements_platform_key_idx").Annotations(entsql.IndexWhere("payer_vendor_id IS NULL")),
		index.Fields("payer_vendor_id", "vendor_id", "idempotency_key").Unique().StorageKey("vendor_settlements_supplier_key_idx").Annotations(entsql.IndexWhere("payer_vendor_id IS NOT NULL")),
		index.Fields("payer_vendor_id", "start_at", "id").StorageKey("vendor_settlements_payer_period_idx").Annotations(entsql.IndexWhere("payer_vendor_id IS NOT NULL")),
		index.Fields("vendor_id", "start_at", "id"),
	}
}
