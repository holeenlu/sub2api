package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

type AccountSupplyIdentityClaim struct{ ent.Schema }

func (AccountSupplyIdentityClaim) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "account_supply_identity_claims"}}
}
func (AccountSupplyIdentityClaim) Mixin() []ent.Mixin { return []ent.Mixin{mixins.TimeMixin{}} }
func (AccountSupplyIdentityClaim) Fields() []ent.Field {
	return []ent.Field{field.String("platform").MaxLen(50), field.String("identity_hash").MaxLen(128), field.Int64("canonical_account_id"), field.Int64("vendor_id").Optional().Nillable()}
}
func (AccountSupplyIdentityClaim) Indexes() []ent.Index {
	return []ent.Index{index.Fields("platform", "identity_hash").Unique()}
}
