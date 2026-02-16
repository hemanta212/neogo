package neogo

import (
	"encoding/json"
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

	t.Run("marshal: non-nil pointer locale with empty fields overwritten from base", func(t *testing.T) {
		// Base is authoritative during marshal: base="Hello" always overwrites locale.
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookNilableLocalePerson{
			Name:       "Hello",
			NameLocale: &hookLocales{EnUS: "", EnAU: ""},
		}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "Hello", person.NameLocale.EnUS,
			"base should overwrite locale during marshal")
	})

	t.Run("marshal: non-zero base overwrites stale non-zero locale", func(t *testing.T) {
		// Base changed from "Old" to "Updated" but locale still has stale data.
		// Marshal hook must overwrite stale locale with new base value.
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookNilableLocalePerson{
			Name:       "Updated",
			NameLocale: &hookLocales{EnUS: "Stale", EnAU: "Stale"},
		}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "Updated", person.NameLocale.EnUS,
			"stale locale should be overwritten from base")
		require.Equal(t, "", person.NameLocale.EnAU,
			"non-preferred locale field should be zeroed")
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

	t.Run("marshal: both non-nil with zero values — locale zeroed from base", func(t *testing.T) {
		// Base is zero (empty string ptr) → locale fields get zeroed out.
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookPtrBaseLocalePerson{
			Name:       strPtr(""),
			NameLocale: &hookLocales{EnUS: "stale", EnAU: "stale"},
		}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "", *person.Name, "base should remain empty")
		require.Equal(t, "", person.NameLocale.EnUS, "locale should be zeroed when base is zero")
		require.Equal(t, "", person.NameLocale.EnAU, "locale should be zeroed when base is zero")
	})

	t.Run("marshal: base zero with non-nil locale — locale gets zeroed", func(t *testing.T) {
		// Base has value "" (zero for string), locale has stale data → locale must be cleared.
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookLocalizedPerson{
			Name:       "",
			NameLocale: hookLocales{EnUS: "stale-US", EnAU: "stale-AU"},
		}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Equal(t, "", person.NameLocale.EnUS, "stale locale should be zeroed when base is zero")
		require.Equal(t, "", person.NameLocale.EnAU, "stale locale should be zeroed when base is zero")
	})

	t.Run("marshal: nil pointer base — locale untouched", func(t *testing.T) {
		// Base is nil pointer → "not provided", locale must not be touched.
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookPtrBaseLocalePerson{
			Name:       nil,
			NameLocale: &hookLocales{EnUS: "existing", EnAU: "data"},
		}
		err := r.applyMarshalHooks(reflect.ValueOf(&person))
		require.NoError(t, err)
		require.Nil(t, person.Name, "nil base should stay nil")
		require.Equal(t, "existing", person.NameLocale.EnUS, "locale should be untouched when base is nil pointer")
		require.Equal(t, "data", person.NameLocale.EnAU, "locale should be untouched when base is nil pointer")
	})
}

// hookHiddenLocalePerson simulates the real-world case where the locale struct
// is tagged json:"-" and therefore invisible to json.Marshal.
type hookHiddenLocalePerson struct {
	Name       string       `json:"name"`
	NameLocale *hookLocales `json:"-"`
}

func TestFlattenLocaleFields(t *testing.T) {
	t.Run("flattens non-nil locale into map", func(t *testing.T) {
		person := hookHiddenLocalePerson{
			Name:       "Hi",
			NameLocale: &hookLocales{EnUS: "US", EnAU: "AU"},
		}
		// JSON round-trip: NameLocale is json:"-" so it won't appear.
		bs, err := json.Marshal(person)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(bs, &m))

		flattenLocaleFields(reflect.ValueOf(person), m)
		require.Equal(t, "US", m["name_enUS"])
		require.Equal(t, "AU", m["name_enAU"])
	})

	t.Run("skips nil locale pointer", func(t *testing.T) {
		person := hookHiddenLocalePerson{
			Name:       "Hi",
			NameLocale: nil,
		}
		bs, err := json.Marshal(person)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(bs, &m))

		flattenLocaleFields(reflect.ValueOf(person), m)
		_, hasUS := m["name_enUS"]
		_, hasAU := m["name_enAU"]
		require.False(t, hasUS, "nil locale should not produce flat keys")
		require.False(t, hasAU, "nil locale should not produce flat keys")
	})

	t.Run("skips zero locale fields when base is non-zero", func(t *testing.T) {
		// When base is "Hi" (non-zero), zero locale fields (EnAU="") should
		// be skipped to preserve other clusters' data in Neo4j.
		person := hookHiddenLocalePerson{
			Name:       "Hi",
			NameLocale: &hookLocales{EnUS: "US", EnAU: ""},
		}
		bs, err := json.Marshal(person)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(bs, &m))

		flattenLocaleFields(reflect.ValueOf(person), m)
		require.Equal(t, "US", m["name_enUS"])
		_, hasAU := m["name_enAU"]
		require.False(t, hasAU, "zero locale field should be skipped when base is non-zero")
	})

	t.Run("emits nil for zero locale fields when base is zero", func(t *testing.T) {
		// When base is "" (zero/empty), user is explicitly clearing the field.
		// All locale fields should be emitted as nil to clear them in Neo4j.
		person := hookHiddenLocalePerson{
			Name:       "",
			NameLocale: &hookLocales{EnUS: "", EnAU: ""},
		}
		bs, err := json.Marshal(person)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(bs, &m))

		flattenLocaleFields(reflect.ValueOf(person), m)
		usVal, hasUS := m["name_enUS"]
		require.True(t, hasUS, "zero locale field should be emitted when base is zero")
		require.Nil(t, usVal, "should emit nil to clear in Neo4j")
		auVal, hasAU := m["name_enAU"]
		require.True(t, hasAU, "zero locale field should be emitted when base is zero")
		require.Nil(t, auVal, "should emit nil to clear in Neo4j")
	})

	t.Run("works with value locale struct", func(t *testing.T) {
		person := hookLocalizedPerson{
			Name:       "Hi",
			NameLocale: hookLocales{EnUS: "US", EnAU: "AU"},
		}
		bs, err := json.Marshal(person)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(bs, &m))

		flattenLocaleFields(reflect.ValueOf(person), m)
		require.Equal(t, "US", m["name_enUS"])
		require.Equal(t, "AU", m["name_enAU"])
	})

	t.Run("handles pointer base field", func(t *testing.T) {
		person := hookPtrBaseLocalePerson{
			Name:       strPtr("Hi"),
			NameLocale: &hookLocales{EnUS: "US"},
		}
		bs, err := json.Marshal(person)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(bs, &m))

		flattenLocaleFields(reflect.ValueOf(person), m)
		require.Equal(t, "US", m["name_enUS"])
	})

	t.Run("clears locale when pointer base is empty string", func(t *testing.T) {
		// Simulates figure="" in UpdateShortQuestionParams
		person := hookPtrBaseLocalePerson{
			Name:       strPtr(""),
			NameLocale: &hookLocales{EnUS: "", EnAU: ""},
		}
		bs, err := json.Marshal(person)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(bs, &m))

		flattenLocaleFields(reflect.ValueOf(person), m)
		usVal, hasUS := m["name_enUS"]
		require.True(t, hasUS, "should emit nil when base ptr is empty string")
		require.Nil(t, usVal)
		auVal, hasAU := m["name_enAU"]
		require.True(t, hasAU, "should emit nil when base ptr is empty string")
		require.Nil(t, auVal)
	})
}

func TestCanonicalizeParamsFlattensLocales(t *testing.T) {
	t.Run("pre-populated locale struct", func(t *testing.T) {
		person := hookHiddenLocalePerson{
			Name:       "Hello",
			NameLocale: &hookLocales{EnUS: "US Val", EnAU: "AU Val"},
		}
		result, err := canonicalizeParams(map[string]any{"props": person}, nil)
		require.NoError(t, err)

		propsRaw, ok := result["props"]
		require.True(t, ok, "result should contain 'props' key")
		props, ok := propsRaw.(map[string]any)
		require.True(t, ok, "props should be map[string]any")
		require.Equal(t, "Hello", props["name"])
		require.Equal(t, "US Val", props["name_enUS"])
		require.Equal(t, "AU Val", props["name_enAU"])
	})

	t.Run("marshal hook populates locale from base on struct value", func(t *testing.T) {
		// Simulates real UpdateSkill flow: struct passed by value with only
		// base field set, locale is nil. The marshal hook must populate locale,
		// then flattenLocaleFields must inject flat keys.
		var r registry
		r.registerMarshalHook(LocalesHook())
		person := hookHiddenLocalePerson{
			Name:       "Hello",
			NameLocale: nil, // hook should fill this
		}
		result, err := canonicalizeParams(
			map[string]any{"props": person},
			r.applyMarshalHooks,
		)
		require.NoError(t, err)

		props, ok := result["props"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "Hello", props["name"])
		require.Equal(t, "Hello", props["name_enUS"],
			"marshal hook should populate EnUS from base, then flatten should inject it")
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
