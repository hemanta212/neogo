package neogo

import (
	"errors"
	"reflect"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
)

type hookPerson struct {
	Name string `json:"name"`
}

type hookWrapper struct {
	Person *hookPerson `json:"person"`
}

type hookIfaceWrapper struct {
	Item any
}

func setHookName(value reflect.Value, next string) bool {
	field := value.FieldByName("Name")
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.String {
		return false
	}
	field.SetString(next)
	return true
}

func TestUnmarshalHook(t *testing.T) {
	var (
		called int
		r      registry
	)
	r.registerAfterUnmarshalHook(func(from any, value reflect.Value) error {
		if setHookName(value, "hooked") {
			called++
		}
		return nil
	})

	person := hookPerson{}
	err := r.bindValue(neo4j.Node{Props: map[string]any{"name": "ignored"}}, reflect.ValueOf(&person))
	require.NoError(t, err)
	require.Equal(t, "hooked", person.Name)

	called = 0
	var people []hookPerson
	props := []any{
		map[string]any{"name": "one"},
		map[string]any{"name": "two"},
	}
	err = r.bindValue(props, reflect.ValueOf(&people))
	require.NoError(t, err)
	require.Len(t, people, 2)
	require.Equal(t, "hooked", people[0].Name)
	require.GreaterOrEqual(t, called, 2)

	called = 0
	var nested [][]hookPerson
	err = r.bindValue([][]any{props}, reflect.ValueOf(&nested))
	require.NoError(t, err)
	require.Len(t, nested, 1)
	require.Equal(t, "hooked", nested[0][0].Name)
	require.GreaterOrEqual(t, called, 2)
}

func TestUnmarshalHookEdgeCases(t *testing.T) {
	t.Run("propagates hook errors", func(t *testing.T) {
		var r registry
		expected := errors.New("boom")
		r.registerAfterUnmarshalHook(func(from any, value reflect.Value) error {
			return expected
		})
		person := hookPerson{}
		err := r.bindValue(map[string]any{"name": "x"}, reflect.ValueOf(&person))
		require.ErrorIs(t, err, expected)
	})

	t.Run("handles nested pointers", func(t *testing.T) {
		var (
			called int
			r      registry
		)
		r.registerAfterUnmarshalHook(func(from any, value reflect.Value) error {
			if setHookName(value, "nested") {
				called++
			}
			return nil
		})
		wrapper := hookWrapper{}
		err := r.bindValue(map[string]any{
			"person": map[string]any{"name": "x"},
		}, reflect.ValueOf(&wrapper))
		require.NoError(t, err)
		require.NotNil(t, wrapper.Person)
		require.Equal(t, "nested", wrapper.Person.Name)
		require.GreaterOrEqual(t, called, 1)
	})

	t.Run("handles interface values", func(t *testing.T) {
		var (
			called int
			r      registry
		)
		r.registerAfterUnmarshalHook(func(from any, value reflect.Value) error {
			if setHookName(value, "iface") {
				called++
			}
			return nil
		})
		wrapper := hookIfaceWrapper{Item: &hookPerson{Name: "x"}}
		err := r.applyAfterUnmarshalHooks(nil, reflect.ValueOf(&wrapper))
		require.NoError(t, err)
		require.Equal(t, "iface", wrapper.Item.(*hookPerson).Name)
		require.GreaterOrEqual(t, called, 1)
	})

	t.Run("applies multiple hooks in order", func(t *testing.T) {
		var r registry
		r.registerAfterUnmarshalHook(func(from any, value reflect.Value) error {
			setHookName(value, "first")
			return nil
		})
		r.registerAfterUnmarshalHook(func(from any, value reflect.Value) error {
			field := value.FieldByName("Name")
			if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.String {
				return nil
			}
			field.SetString(field.String() + "-second")
			return nil
		})
		person := hookPerson{}
		err := r.bindValue(map[string]any{"name": "x"}, reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "first-second", person.Name)
	})
}

func TestAfterMarshalHook(t *testing.T) {
	t.Run("modifies serialized struct map", func(t *testing.T) {
		var called int
		var r registry
		r.registerAfterMarshalHook(func(key string, original reflect.Value, serialized map[string]any) error {
			if _, ok := serialized["name"]; ok {
				serialized["name"] = "hooked"
				called++
			}
			return nil
		})
		result, err := canonicalizeParams(
			map[string]any{"props": hookPerson{Name: "raw"}},
			r.applyAfterMarshalHooks,
		)
		require.NoError(t, err)
		props := result["props"].(map[string]any)
		require.Equal(t, "hooked", props["name"])
		require.Equal(t, 1, called)
	})

	t.Run("fires per element for slice of structs", func(t *testing.T) {
		var called int
		var r registry
		r.registerAfterMarshalHook(func(key string, original reflect.Value, serialized map[string]any) error {
			if name, ok := serialized["name"]; ok {
				serialized["name"] = name.(string) + "-hooked"
				called++
			}
			return nil
		})
		people := []hookPerson{{Name: "Alice"}, {Name: "Bob"}}
		result, err := canonicalizeParams(
			map[string]any{"props": people},
			r.applyAfterMarshalHooks,
		)
		require.NoError(t, err)
		props := result["props"].([]any)
		require.Len(t, props, 2)
		require.Equal(t, "Alice-hooked", props[0].(map[string]any)["name"])
		require.Equal(t, "Bob-hooked", props[1].(map[string]any)["name"])
		require.Equal(t, 2, called)
	})

	t.Run("propagates hook errors", func(t *testing.T) {
		expected := errors.New("hook failed")
		var r registry
		r.registerAfterMarshalHook(func(key string, original reflect.Value, serialized map[string]any) error {
			return expected
		})
		_, err := canonicalizeParams(
			map[string]any{"props": hookPerson{Name: "test"}},
			r.applyAfterMarshalHooks,
		)
		require.ErrorIs(t, err, expected)
	})

	t.Run("propagates hook errors for slice elements", func(t *testing.T) {
		expected := errors.New("slice hook failed")
		var r registry
		r.registerAfterMarshalHook(func(key string, original reflect.Value, serialized map[string]any) error {
			return expected
		})
		_, err := canonicalizeParams(
			map[string]any{"props": []hookPerson{{Name: "test"}}},
			r.applyAfterMarshalHooks,
		)
		require.ErrorIs(t, err, expected)
	})

	t.Run("receives param key name", func(t *testing.T) {
		var receivedKey string
		var r registry
		r.registerAfterMarshalHook(func(key string, original reflect.Value, serialized map[string]any) error {
			receivedKey = key
			return nil
		})
		_, err := canonicalizeParams(
			map[string]any{"myParam": hookPerson{Name: "test"}},
			r.applyAfterMarshalHooks,
		)
		require.NoError(t, err)
		require.Equal(t, "myParam", receivedKey)
	})

	t.Run("can read original struct fields including json-hidden", func(t *testing.T) {
		type hiddenField struct {
			Name   string `json:"name"`
			Secret string `json:"-"`
		}
		var r registry
		r.registerAfterMarshalHook(func(key string, original reflect.Value, serialized map[string]any) error {
			if secret := original.FieldByName("Secret"); secret.IsValid() {
				serialized["secret_value"] = secret.String()
			}
			return nil
		})
		result, err := canonicalizeParams(
			map[string]any{"props": hiddenField{Name: "visible", Secret: "hidden"}},
			r.applyAfterMarshalHooks,
		)
		require.NoError(t, err)
		props := result["props"].(map[string]any)
		require.Equal(t, "visible", props["name"])
		require.Equal(t, "hidden", props["secret_value"])
	})
}
