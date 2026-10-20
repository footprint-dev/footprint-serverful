package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type DeviceAnalysis struct{ ent.Schema }

func (DeviceAnalysis) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("device").NotEmpty(),
		field.Int64("revision").NonNegative(),
	}
}
