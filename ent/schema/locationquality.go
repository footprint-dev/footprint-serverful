package schema

import (
	"encoding/json"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type LocationQuality struct{ ent.Schema }

func (LocationQuality) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("location_id").Unique(),
		field.Enum("status").Values("pending", "retained", "outlier", "review", "uncertain"),
		field.String("reason"),
		field.String("algorithm_version"),
		field.Int64("revision").Positive(),
		field.Int64("checked_at"),
		field.JSON("evidence", json.RawMessage{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (LocationQuality) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("location", Location.Type).Ref("quality").Field("location_id").Unique().Required(),
	}
}
