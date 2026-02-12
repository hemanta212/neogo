package neogo

import (
	"errors"
	"reflect"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"

	"github.com/rlch/neogo/db"
	"github.com/rlch/neogo/internal"
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

type hookLocales struct {
	EnUS string `json:"enUS"`
	EnAU string `json:"enAU"`
}

type hookLocalizedPerson struct {
	Name       string      `json:"name"`
	NameLocale hookLocales `json:"nameLocale"`
}

// Pointer locale struct — nil means "not provided", non-nil zero struct means "all fields explicitly empty"
type hookNilableLocalePerson struct {
	Name       string       `json:"name"`
	NameLocale *hookLocales `json:"nameLocale"`
}

// Pointer base + pointer locale — both support nil-vs-zero distinction
type hookPtrBaseLocalePerson struct {
	Name       *string      `json:"name"`
	NameLocale *hookLocales `json:"nameLocale"`
}

func strPtr(s string) *string { return &s }

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
	r.registerUnmarshalHook(func(from any, value reflect.Value) error {
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
		r.registerUnmarshalHook(func(from any, value reflect.Value) error {
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
		r.registerUnmarshalHook(func(from any, value reflect.Value) error {
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
		r.registerUnmarshalHook(func(from any, value reflect.Value) error {
			if setHookName(value, "iface") {
				called++
			}
			return nil
		})
		wrapper := hookIfaceWrapper{Item: &hookPerson{Name: "x"}}
		err := r.applyUnmarshalHooks(nil, reflect.ValueOf(&wrapper))
		require.NoError(t, err)
		require.Equal(t, "iface", wrapper.Item.(*hookPerson).Name)
		require.GreaterOrEqual(t, called, 1)
	})

	t.Run("applies multiple hooks in order", func(t *testing.T) {
		var r registry
		r.registerUnmarshalHook(func(from any, value reflect.Value) error {
			setHookName(value, "first")
			return nil
		})
		r.registerUnmarshalHook(func(from any, value reflect.Value) error {
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

func TestMarshalHook(t *testing.T) {
	var called int
	c := internal.NewCypherClient()
	c.Scope.SetMarshalHook(func(value reflect.Value) error {
		if value.Kind() == reflect.Struct {
			if field := value.FieldByName("Name"); field.IsValid() && field.CanSet() {
				field.SetString("hooked")
				called++
			}
		}
		return nil
	})

	person := hookPerson{Name: "raw"}
	cy, err := c.
		Create(db.Node(db.Qual(&person, "n"))).
		Return(&person).
		Compile()
	require.NoError(t, err)
	require.Equal(t, "hooked", cy.Parameters["n_name"])
	require.Equal(t, 1, called)
}

func TestLocalesHook(t *testing.T) {
	t.Run("fills base from locale on unmarshal", func(t *testing.T) {
		var r registry
		r.registerUnmarshalHook(LocalesUnmarshalHook())
		person := hookLocalizedPerson{}
		err := r.bindValue(map[string]any{
			"nameLocale": map[string]any{"enUS": "Hello"},
		}, reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "Hello", person.Name)
		require.Equal(t, "Hello", person.NameLocale.EnUS)
	})

	t.Run("fills locale from base on marshal", func(t *testing.T) {
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookLocalizedPerson{Name: "Hi"}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "Hi", person.NameLocale.EnUS)
	})

	t.Run("prefers selected locale on unmarshal", func(t *testing.T) {
		var r registry
		r.registerUnmarshalHook(LocalesUnmarshalHookWithSelector(staticLocaleSelector{"EnAU", "EnUS"}))
		person := hookLocalizedPerson{}
		err := r.bindValue(map[string]any{
			"nameLocale": map[string]any{"enUS": "US", "enAU": "AU"},
		}, reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "AU", person.Name)
	})

	t.Run("fills selected locale on marshal", func(t *testing.T) {
		var r registry
		r.registerMarshalHook(LocalesHookWithSelector(staticLocaleSelector{"EnAU", "EnUS"}))
		person := hookLocalizedPerson{Name: "Hi"}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "Hi", person.NameLocale.EnAU)
		require.Empty(t, person.NameLocale.EnUS)
	})

	t.Run("extracts flat locale keys from raw props", func(t *testing.T) {
		var r registry
		r.registerUnmarshalHook(LocalesUnmarshalHook())
		person := hookLocalizedPerson{}
		err := r.bindValue(map[string]any{
			"name":      "fallback",
			"name_enUS": "US Value",
			"name_enAU": "AU Value",
		}, reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "US Value", person.NameLocale.EnUS)
		require.Equal(t, "AU Value", person.NameLocale.EnAU)
		// Base should be set from preferred locale (EnUS first by default)
		require.Equal(t, "US Value", person.Name)
	})

	t.Run("extracts flat keys with AU preference", func(t *testing.T) {
		var r registry
		r.registerUnmarshalHook(LocalesUnmarshalHookWithSelector(staticLocaleSelector{"EnAU", "EnUS"}))
		person := hookLocalizedPerson{}
		err := r.bindValue(map[string]any{
			"name":      "fallback",
			"name_enUS": "US Value",
			"name_enAU": "AU Value",
		}, reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "US Value", person.NameLocale.EnUS)
		require.Equal(t, "AU Value", person.NameLocale.EnAU)
		// Base should be set from preferred locale (EnAU first)
		require.Equal(t, "AU Value", person.Name)
	})

	t.Run("extracts flat keys with pointer locale struct", func(t *testing.T) {
		var r registry
		r.registerUnmarshalHook(LocalesUnmarshalHookWithSelector(staticLocaleSelector{"EnUS", "EnAU"}))
		person := hookNilableLocalePerson{}
		err := r.bindValue(map[string]any{
			"name":      "fallback",
			"name_enUS": "Hello US",
		}, reflect.ValueOf(&person))
		require.NoError(t, err)
		require.NotNil(t, person.NameLocale, "nil pointer locale should be allocated when flat keys exist")
		require.Equal(t, "Hello US", person.NameLocale.EnUS)
		require.Equal(t, "Hello US", person.Name)
	})

	t.Run("no flat keys leaves pointer locale nil", func(t *testing.T) {
		var r registry
		r.registerUnmarshalHook(LocalesUnmarshalHookWithSelector(staticLocaleSelector{"EnUS", "EnAU"}))
		person := hookNilableLocalePerson{}
		err := r.bindValue(map[string]any{
			"name": "Hello",
		}, reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Nil(t, person.NameLocale, "pointer locale should stay nil when no flat keys present")
		require.Equal(t, "Hello", person.Name)
	})
}

// TestLocalesHookZeroValuePreservation exercises nil-vs-zero semantics.
// A non-nil pointer to a zero-value struct/field means "explicitly set to empty" and
// must be preserved. Only nil pointers mean "not provided" and should trigger fallback.
func TestLocalesHookZeroValuePreservation(t *testing.T) {
	// --- Marshal direction: base -> locale ---

	t.Run("marshal: non-nil pointer locale with empty fields NOT overwritten from base", func(t *testing.T) {
		// NameLocale is explicitly &hookLocales{EnUS:"", EnAU:""} — caller said "all locales are empty".
		// The hook must NOT overwrite these empty strings with base value.
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookNilableLocalePerson{
			Name:       "Hello",
			NameLocale: &hookLocales{EnUS: "", EnAU: ""},
		}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "", person.NameLocale.EnUS,
			"explicitly provided empty locale field should not be overwritten from base")
		require.Equal(t, "", person.NameLocale.EnAU,
			"explicitly provided empty locale field should not be overwritten from base")
	})

	t.Run("marshal: nil pointer locale gets filled from base", func(t *testing.T) {
		// NameLocale is nil — locale was never set — should be allocated and filled from base.
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookNilableLocalePerson{
			Name:       "Hello",
			NameLocale: nil,
		}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.NotNil(t, person.NameLocale)
		require.Equal(t, "Hello", person.NameLocale.EnUS)
	})

	// --- Unmarshal direction: locale -> base ---

	t.Run("unmarshal: non-nil pointer base with empty string NOT overwritten from locale", func(t *testing.T) {
		// Name is ptr("") — caller explicitly set base to empty string.
		// The hook must NOT overwrite it with a locale value.
		var r registry
		r.registerUnmarshalHook(LocalesUnmarshalHook())
		person := hookPtrBaseLocalePerson{
			Name:       strPtr(""),
			NameLocale: &hookLocales{EnUS: "Hello"},
		}
		err := r.applyUnmarshalHooks(nil, reflect.ValueOf(&person))
		require.NoError(t, err)
		require.NotNil(t, person.Name)
		require.Equal(t, "", *person.Name,
			"explicitly empty base should not be overwritten from locale")
	})

	t.Run("unmarshal: nil pointer base gets filled from locale", func(t *testing.T) {
		// Name is nil — base was never set — should be allocated and filled from locale.
		var r registry
		r.registerUnmarshalHook(LocalesUnmarshalHook())
		person := hookPtrBaseLocalePerson{
			Name:       nil,
			NameLocale: &hookLocales{EnUS: "Hello"},
		}
		err := r.applyUnmarshalHooks(nil, reflect.ValueOf(&person))
		require.NoError(t, err)
		require.NotNil(t, person.Name)
		require.Equal(t, "Hello", *person.Name)
	})

	// --- Both directions: mutual zero-value preservation ---

	t.Run("marshal: both non-nil with zero values — neither overwritten", func(t *testing.T) {
		// Both base and locale are explicitly provided with empty/zero values.
		// Neither should overwrite the other.
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookPtrBaseLocalePerson{
			Name:       strPtr(""),
			NameLocale: &hookLocales{EnUS: "", EnAU: ""},
		}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "", *person.Name, "base should remain empty")
		require.Equal(t, "", person.NameLocale.EnUS, "locale should remain empty")
		require.Equal(t, "", person.NameLocale.EnAU, "locale should remain empty")
	})
}

// TestMarshalZeroValueFieldsPreserved verifies that zero-value struct fields
// are included in Cypher parameters (not silently dropped).
// This tests scope.go's bindFieldsFrom which skips f.IsZero() fields.
func TestMarshalZeroValueFieldsPreserved(t *testing.T) {
	c := internal.NewCypherClient()
	person := hookPerson{Name: ""}
	cy, err := c.
		Create(db.Node(db.Qual(&person, "n"))).
		Return(&person).
		Compile()
	require.NoError(t, err)
	_, exists := cy.Parameters["n_name"]
	require.True(t, exists,
		"zero-value field should still be included in Cypher parameters")
}
