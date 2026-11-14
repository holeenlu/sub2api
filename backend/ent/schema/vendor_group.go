package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type VendorGroup struct{ ent.Schema }

func (VendorGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "vendor_groups"}}
}
func (VendorGroup) Fields() []ent.Field {
	return []ent.Field{field.Int64("vendor_id"), field.Int64("group_id"), field.Bool("is_default").Default(false)}
}
func (VendorGroup) Indexes() []ent.Index {
	return []ent.Index{index.Fields("vendor_id", "group_id").Unique()}
}
