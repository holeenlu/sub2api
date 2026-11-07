package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// VendorSettlementLine preserves supplier accounting independently of raw usage retention.
type VendorSettlementLine struct{ ent.Schema }

func (VendorSettlementLine) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "vendor_settlement_lines"}}
}
func (VendorSettlementLine) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("statement_id"),
		field.Int64("group_id").Optional().Nillable(),
		field.String("group_name").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("contract_version"),
		field.String("coefficient").SchemaType(map[string]string{dialect.Postgres: "numeric(30,10)"}),
		field.String("cost_usd").SchemaType(map[string]string{dialect.Postgres: "numeric(38,20)"}),
		field.String("amount").SchemaType(map[string]string{dialect.Postgres: "numeric(30,2)"}),
		field.Int64("usage_count"),
		field.String("kind"),
		field.String("reason").SchemaType(map[string]string{dialect.Postgres: "text"}),
	}
}
func (VendorSettlementLine) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("statement_id", "id").Unique(),
	}
}
