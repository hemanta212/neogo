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
	r.registerUnmarshalHook(func(value reflect.Value) error {
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
		r.registerUnmarshalHook(func(value reflect.Value) error {
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
		r.registerUnmarshalHook(func(value reflect.Value) error {
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
		r.registerUnmarshalHook(func(value reflect.Value) error {
			if setHookName(value, "iface") {
				called++
			}
			return nil
		})
		wrapper := hookIfaceWrapper{Item: &hookPerson{Name: "x"}}
		err := r.applyUnmarshalHooks(reflect.ValueOf(&wrapper))
		require.NoError(t, err)
		require.Equal(t, "iface", wrapper.Item.(*hookPerson).Name)
		require.GreaterOrEqual(t, called, 1)
	})

	t.Run("applies multiple hooks in order", func(t *testing.T) {
		var r registry
		r.registerUnmarshalHook(func(value reflect.Value) error {
			setHookName(value, "first")
			return nil
		})
		r.registerUnmarshalHook(func(value reflect.Value) error {
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
		r.registerUnmarshalHook(LocalesHook())
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
		r.registerUnmarshalHook(LocalesHookWithSelector(staticLocaleSelector{"EnAU", "EnUS"}))
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
}
