package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

type Vendor struct{ ent.Schema }

func (Vendor) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "vendors"}}
}
func (Vendor) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }
func (Vendor) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"), field.Int16("tier").Default(1).Immutable(), field.Int64("parent_vendor_id").Optional().Nillable().Immutable(), field.String("name").MaxLen(100), field.String("status").Default("active"), field.Int64("policy_version").Default(1),
		field.JSON("platforms", []string{}), field.JSON("openai_authorization_modes", []string{}).Default([]string{"native"}), field.Int("account_limit").Default(0), field.Int("proxy_limit").Default(0), field.Bool("allow_direct").Default(false),
		field.Int("default_concurrency").Default(15), field.Int("default_priority").Default(1), field.Float("default_rate_multiplier").Default(1).SchemaType(map[string]string{"postgres": "numeric(20,8)"}),
		field.String("contact_name").Default(""), field.String("settlement_email").Default(""), field.String("binance_id").Default(""),
		field.String("default_coefficient").Default("1").SchemaType(map[string]string{"postgres": "numeric(20,8)"}), field.JSON("group_coefficients", map[string]string{}), field.Int64("contract_version").Default(1),
	}
}
func (Vendor) Indexes() []ent.Index { return []ent.Index{index.Fields("user_id").Unique()} }
