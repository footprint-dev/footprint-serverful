package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Project struct{ ent.Schema }

func (Project) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("name").NotEmpty(),
		field.Int64("start_time").NonNegative().Max(8_640_000_000_000_000),
		field.Int64("end_time").NonNegative().Max(8_640_000_000_000_000),
	}
}

func (Project) Indexes() []ent.Index {
	return []ent.Index{index.Fields("name", "id"), index.Fields("start_time", "id")}
}
