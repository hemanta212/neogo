package internal

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type Person struct {
	Node       `neo4j:"Person"`
	Name       string  `json:"name"`
	NameLocale Locales `json:"name,flatten"`
}

type Locales struct {
	EnUS string `json:"enUS"`
}

type nestedOuter struct {
	Inner nestedInner `json:"outer,flatten"`
}

type nestedInner struct {
	Leaf nestedLeaf `json:"inner,flatten"`
}

type nestedLeaf struct {
	Value string `json:"value"`
}

func TestBindFields(t *testing.T) {
	t.Run("binds composite fields", func(t *testing.T) {
		s := newScope()
		p := &Person{}
		s.bindFields(reflect.ValueOf(p).Elem(), "p")
		require.Equal(t, map[uintptr]field{
			reflect.ValueOf(&p.ID).Pointer(): {
				identifier: "p",
				name:       "id",
			},
			reflect.ValueOf(&p.Name).Pointer(): {
				identifier: "p",
				name:       "name",
			},
			reflect.ValueOf(&p.NameLocale.EnUS).Pointer(): {
				identifier: "p",
				name:       "name_enUS",
			},
		}, s.fields)
		require.Equal(t, map[reflect.Value]string{
			reflect.ValueOf(&p.ID):              "p.id",
			reflect.ValueOf(&p.Name):            "p.name",
			reflect.ValueOf(&p.NameLocale.EnUS): "p.name_enUS",
		}, s.names)
	})

	t.Run("binds nested flatten fields", func(t *testing.T) {
		outer := &nestedOuter{}
		s := newScope()
		s.bindFields(reflect.ValueOf(outer).Elem(), "o")
		leafPtr := reflect.ValueOf(&outer.Inner.Leaf.Value)
		require.Equal(t, "o.outer_inner_value", s.names[leafPtr])
	})

	t.Run("ignores nil anonymous pointer", func(t *testing.T) {
		type Embedded struct {
			Value string `json:"value"`
		}
		type Wrapper struct {
			*Embedded
		}
		w := &Wrapper{}
		s := newScope()
		require.NotPanics(t, func() {
			s.bindFields(reflect.ValueOf(w).Elem(), "w")
		})
	})
}
