package neogo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testLocale struct {
	EnUS string `db:"enUS"`
	EnAU string `db:"enAU"`
}

type testDBStruct struct {
	Title       string      `db:"title"`
	TitleLocale *testLocale `db:"title,flatten"`
	Ignored     string      `json:"-"`
}

type testJSONStruct struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type testNoTagStruct struct {
	Name  string
	Count int
}

type testEmbeddedBase struct {
	Code string `db:"code"`
}

type testEmbeddedStruct struct {
	testEmbeddedBase
	Name string `db:"name"`
}

func TestCanonicalizeParams(t *testing.T) {
	t.Run("struct with db tags uses PropsFromStruct", func(t *testing.T) {
		input := testDBStruct{
			Title: "Hello",
			TitleLocale: &testLocale{
				EnUS: "Hello-US",
				EnAU: "Hello-AU",
			},
			Ignored: "skip",
		}
		params, err := canonicalizeParams(map[string]any{"props": input})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"props": map[string]any{
				"title":      "Hello",
				"title_enUS": "Hello-US",
				"title_enAU": "Hello-AU",
			},
		}, params)
	})

	t.Run("pointer to struct with db tags uses PropsFromStruct", func(t *testing.T) {
		input := &testDBStruct{
			Title: "Hello",
			TitleLocale: &testLocale{
				EnUS: "Hello-US",
			},
		}
		params, err := canonicalizeParams(map[string]any{"props": input})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"props": map[string]any{
				"title":      "Hello",
				"title_enUS": "Hello-US",
			},
		}, params)
	})

	t.Run("nil pointer struct becomes nil", func(t *testing.T) {
		var input *testDBStruct
		params, err := canonicalizeParams(map[string]any{"props": input})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"props": nil}, params)
	})

	t.Run("struct with json tags uses PropsFromStruct and omits zero values", func(t *testing.T) {
		input := testJSONStruct{Name: "Alpha"}
		params, err := canonicalizeParams(map[string]any{"props": input})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"props": map[string]any{"name": "Alpha"},
		}, params)
	})

	t.Run("struct without tags uses JSON marshal", func(t *testing.T) {
		input := testNoTagStruct{Name: "Alpha", Count: 2}
		params, err := canonicalizeParams(map[string]any{"props": input})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"props": map[string]any{
				"Name":  "Alpha",
				"Count": float64(2),
			},
		}, params)
	})

	t.Run("unexported embedded struct fields are skipped", func(t *testing.T) {
		input := testEmbeddedStruct{
			testEmbeddedBase: testEmbeddedBase{Code: "X"},
			Name:             "Alpha",
		}
		params, err := canonicalizeParams(map[string]any{"props": input})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"props": map[string]any{
				"name": "Alpha",
			},
		}, params)
	})

	t.Run("map uses JSON marshal", func(t *testing.T) {
		params, err := canonicalizeParams(map[string]any{
			"props": map[string]any{"count": 2},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"props": map[string]any{"count": float64(2)},
		}, params)
	})

	t.Run("slice uses JSON marshal", func(t *testing.T) {
		params, err := canonicalizeParams(map[string]any{
			"props": []int{1, 2},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"props": []any{float64(1), float64(2)},
		}, params)
	})
}
