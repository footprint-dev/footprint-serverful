package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Location struct{ ent.Schema }

func (Location) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Float("longitude").Min(-180).Max(180),
		field.Float("latitude").Min(-90).Max(90),
		field.Float("altitude").Default(0),
		field.String("city").Default(""),
		field.String("device").NotEmpty(),
		field.Int64("timestamp").NonNegative().Max(8_640_000_000_000_000),
		field.Float("timezone").Optional().Nillable().Min(-12).Max(14),
	}
}

func (Location) Edges() []ent.Edge {
	return []ent.Edge{edge.To("quality", LocationQuality.Type).Unique().Annotations(entsql.OnDelete(entsql.Cascade))}
}

func (Location) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("timestamp", "id"),
		index.Fields("device", "timestamp", "id"),
		index.Fields("device", "latitude", "longitude", "timestamp", "id"),
	}
}
