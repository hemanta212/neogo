package internal

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

type organism interface {
	IAbstract
}

type baseOrganism struct {
	Abstract `neo4j:"Organism"`
	Node
}

func (baseOrganism) Implementers() []IAbstract {
	return []IAbstract{}
}

type person struct {
	Node `neo4j:"Person"`
}

type swedishPerson struct {
	person `neo4j:"Swedish"`
}

type foreignPerson struct {
	swedishPerson `neo4j:"Foreign"`
}

type personWithNonStructLabels struct {
	swedishPerson
	Name string `neo4j:"Name"`
}

type personWithAnonymousStructLabels struct {
	swedishPerson
	S swedishPerson `neo4j:"Concrete"`
}

type robot struct {
	Label `neo4j:"Robot"`
}

func TestExtractNodeLabel(t *testing.T) {
	t.Run("nil when node nil", func(t *testing.T) {
		assert.Nil(t, ExtractNodeLabels(nil))
	})

	t.Run("extracts node label", func(t *testing.T) {
		assert.Equal(t, []string{"Person"}, ExtractNodeLabels(person{}))
	})

	t.Run("postpends nested node labels", func(t *testing.T) {
		assert.Equal(t, []string{"Person", "Swedish", "Foreign"}, ExtractNodeLabels(foreignPerson{}))
	})

	t.Run("extract node label from slice of node", func(t *testing.T) {
		assert.Equal(t, []string{"Person"}, ExtractNodeLabels([]*person{}))
	})

	t.Run("extract node label from pointer to slice of node", func(t *testing.T) {
		assert.Equal(t, []string{"Person"}, ExtractNodeLabels(&[]*person{}))
	})

	t.Run("only extract the labels from structs", func(t *testing.T) {
		assert.Equal(t, []string{"Person", "Swedish"}, ExtractNodeLabels(personWithNonStructLabels{}))
	})

	t.Run("only extract the labels from anonymous structs", func(t *testing.T) {
		assert.Equal(t, []string{"Person", "Swedish"}, ExtractNodeLabels(personWithAnonymousStructLabels{}))
	})

	t.Run("extracts from abstract types", func(t *testing.T) {
		var o organism = &baseOrganism{}
		assert.Equal(t, []string{"Organism"}, ExtractNodeLabels(o))
	})

	t.Run("extracts from pointers to abstract types", func(t *testing.T) {
		var o organism = &baseOrganism{}
		o1 := &o
		o2 := &o1
		assert.Equal(t, []string{"Organism"}, ExtractNodeLabels(o2))
	})

	t.Run("extracts from structs embedding Label, ordered by DFS", func(t *testing.T) {
		swedishRobot := struct {
			swedishPerson
			robot
		}{}
		assert.Equal(t, []string{"Person", "Robot", "Swedish"}, ExtractNodeLabels(swedishRobot))
	})
}

type friendship struct {
	Relationship `neo4j:"Friendship"`
}

type family struct {
	Relationship `neo4j:"Family"`
}

func TestExtractRelationshipType(t *testing.T) {
	t.Run("empty string when relationship nil", func(t *testing.T) {
		assert.Equal(t, "", ExtractRelationshipType(nil))
	})

	t.Run("extracts relationship type", func(t *testing.T) {
		assert.Equal(t, "Friendship", ExtractRelationshipType(friendship{}))
	})

	t.Run("panic on multiple relationship types", func(t *testing.T) {
		typ := ExtractRelationshipType([]interface{}{friendship{}, family{}})
		assert.Equal(t, "", typ)
	})

	t.Run("empty string when relationship type is not found", func(t *testing.T) {
		assert.Equal(t, "", ExtractRelationshipType(person{}))
	})

	t.Run("extract relationship type from slice of relationship", func(t *testing.T) {
		assert.Equal(t, "Friendship", ExtractRelationshipType([]*friendship{}))
	})

	t.Run("extract relationship type from pointer to slice of relationship", func(t *testing.T) {
		assert.Equal(t, "Friendship", ExtractRelationshipType(&[]*friendship{}))
	})
}

type propTagExample struct {
	Name      string `json:"name"`
	DBName    string `db:"dbName" json:"ignored"`
	Flattened string `db:",flatten"`
	Ignored   string `json:"-"`
}

type flattenStruct struct {
	Value string
}

func TestPropTagForField(t *testing.T) {
	t.Run("db tag takes precedence", func(t *testing.T) {
		f, _ := reflect.TypeOf(propTagExample{}).FieldByName("DBName")
		tag, ok := PropTagForField(f)
		assert.True(t, ok)
		assert.Equal(t, "db", tag.TagKey)
		assert.Equal(t, "dbName", tag.Name)
		assert.False(t, tag.Flatten)
		assert.False(t, tag.Ignore)
	})

	t.Run("flatten with empty name", func(t *testing.T) {
		f, _ := reflect.TypeOf(propTagExample{}).FieldByName("Flattened")
		tag, ok := PropTagForField(f)
		assert.True(t, ok)
		assert.Equal(t, "", tag.Name)
		assert.True(t, tag.Flatten)
	})

	t.Run("parse ignores field name", func(t *testing.T) {
		tag := ParsePropTag("db", ",flatten")
		assert.Equal(t, "", tag.Name)
		assert.True(t, tag.Flatten)
	})

	t.Run("ignore tag", func(t *testing.T) {
		f, _ := reflect.TypeOf(propTagExample{}).FieldByName("Ignored")
		tag, ok := PropTagForField(f)
		assert.True(t, ok)
		assert.True(t, tag.Ignore)
	})

	t.Run("json tag fallback", func(t *testing.T) {
		f, _ := reflect.TypeOf(propTagExample{}).FieldByName("Name")
		tag, ok := PropTagForField(f)
		assert.True(t, ok)
		assert.Equal(t, "json", tag.TagKey)
		assert.Equal(t, "name", tag.Name)
	})

	t.Run("validate flatten type", func(t *testing.T) {
		type NotStruct string
		assert.NoError(t, ValidateFlattenType(reflect.TypeOf(flattenStruct{})))
		assert.NoError(t, ValidateFlattenType(reflect.TypeOf(&flattenStruct{})))
		assert.Error(t, ValidateFlattenType(reflect.TypeOf(NotStruct(""))))
	})
}
