package neogo

import "reflect"

// AfterMarshalHook runs after a struct parameter is serialized to map[string]any
// but before the map is sent to Neo4j. It receives the parameter key name,
// the original struct value, and the serialized map for modification.
type AfterMarshalHook func(key string, original reflect.Value, serialized map[string]any) error

// AfterUnmarshalHook runs after values are unmarshalled from Neo4j results.
// `from` is the raw source (typically map[string]any of node properties).
// `to` is the deserialized struct value.
type AfterUnmarshalHook func(from any, to reflect.Value) error
