package neogo

import (
	"context"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/rlch/neogo/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Test entity types ────────────────────────────────────────────────────────

type localeTestLocales struct {
	EnUS string `json:"enUS,omitempty" db:"enUS"`
	EnAU string `json:"enAU,omitempty" db:"enAU"`
}

// Simulates a Skill / Topic entity with a single locale field.
type localeTestNode struct {
	Node  `neo4j:"LocaleTestNode"`
	Title       string             `json:"title"`
	TitleLocale *localeTestLocales `json:"-"`
}

// Simulates UpdateSkillInput — pointer base, omitempty, locale hidden.
type localeTestUpdateParams struct {
	Title       *string            `json:"title,omitempty"`
	TitleLocale *localeTestLocales `json:"-"`
}

// Simulates a Question entity with two locale fields.
type localeTestQuestion struct {
	Node    `neo4j:"LocaleTestQuestion"`
	Content       string             `json:"content"`
	ContentLocale *localeTestLocales `json:"-"`
	Figure        string             `json:"figure"`
	FigureLocale  *localeTestLocales `json:"-"`
}

// Simulates UpdateShortQuestionParams — pointer base fields.
type localeTestQuestionUpdate struct {
	Content       *string            `json:"content,omitempty"`
	ContentLocale *localeTestLocales `json:"-"`
	Figure        *string            `json:"figure,omitempty"`
	FigureLocale  *localeTestLocales `json:"-"`
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func newLocaleDriver(t *testing.T, ctx context.Context, preferredKeys []string) Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("locale E2E tests require local Neo4j on port 7687")
	}
	uri, cancel := startNeo4J(ctx)
	selector := staticLocaleSelector(preferredKeys)
	d, err := New(uri, neo4j.BasicAuth("neo4j", "password", ""),
		WithLocales(selector),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		// Clean up all test nodes
		_ = d.Exec().Cypher(`MATCH (n:LocaleTestNode) DETACH DELETE n`).Run(ctx)
		_ = d.Exec().Cypher(`MATCH (n:LocaleTestQuestion) DETACH DELETE n`).Run(ctx)
		_ = cancel(ctx)
	})
	return d
}

// rawProps fetches all properties of a node by ID via a raw neo4j session,
// bypassing neogo hooks. This is the ground truth for what's in the DB.
func rawProps(t *testing.T, ctx context.Context, d Driver, label, id string) map[string]any {
	t.Helper()
	session := d.DB().NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	result, err := session.Run(ctx,
		"MATCH (n:"+label+" {id: $id}) RETURN properties(n) AS props",
		map[string]any{"id": id},
	)
	require.NoError(t, err)
	rec, err := result.Single(ctx)
	require.NoError(t, err)
	raw, _ := rec.Get("props")
	return raw.(map[string]any)
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestLocaleE2E(t *testing.T) {
	ctx := context.Background()

	t.Run("AU cluster", func(t *testing.T) {
		d := newLocaleDriver(t, ctx, []string{"EnAU", "EnUS"})

		t.Run("create writes base + preferred locale only", func(t *testing.T) {
			n := localeTestNode{Title: "Algebra"}
			n.ID = "locale-create-1"
			err := d.Exec().
				Cypher(`CREATE (n:LocaleTestNode) SET n = {id: $id}, n += $props`).
				Return(db.Qual(&n, "n")).
				RunWithParams(ctx, map[string]any{"id": n.ID, "props": n})
			require.NoError(t, err)

			props := rawProps(t, ctx, d, "LocaleTestNode", "locale-create-1")
			assert.Equal(t, "Algebra", props["title"])
			assert.Equal(t, "Algebra", props["title_enAU"], "preferred locale should be written")
			_, hasUS := props["title_enUS"]
			assert.False(t, hasUS, "non-preferred locale key must not exist in DB")
		})

		t.Run("update propagates new value to preferred locale", func(t *testing.T) {
			params := localeTestUpdateParams{Title: strPtr("Geometry")}
			err := d.Exec().
				Cypher(`MATCH (n:LocaleTestNode {id: $id}) SET n += $props`).
				RunWithParams(ctx, map[string]any{"id": "locale-create-1", "props": params})
			require.NoError(t, err)

			props := rawProps(t, ctx, d, "LocaleTestNode", "locale-create-1")
			assert.Equal(t, "Geometry", props["title"])
			assert.Equal(t, "Geometry", props["title_enAU"])
			_, hasUS := props["title_enUS"]
			assert.False(t, hasUS, "non-preferred key must not appear after update")
		})

		t.Run("empty string propagates to preferred locale", func(t *testing.T) {
			params := localeTestUpdateParams{Title: strPtr("")}
			err := d.Exec().
				Cypher(`MATCH (n:LocaleTestNode {id: $id}) SET n += $props`).
				RunWithParams(ctx, map[string]any{"id": "locale-create-1", "props": params})
			require.NoError(t, err)

			props := rawProps(t, ctx, d, "LocaleTestNode", "locale-create-1")
			assert.Equal(t, "", props["title"])
			assert.Equal(t, "", props["title_enAU"], "empty string must propagate to locale")
		})

		t.Run("nil pointer field preserves existing locale", func(t *testing.T) {
			// First set a known value
			setup := localeTestUpdateParams{Title: strPtr("Calculus")}
			err := d.Exec().
				Cypher(`MATCH (n:LocaleTestNode {id: $id}) SET n += $props`).
				RunWithParams(ctx, map[string]any{"id": "locale-create-1", "props": setup})
			require.NoError(t, err)

			// Update with nil Title (field not provided)
			params := localeTestUpdateParams{Title: nil}
			err = d.Exec().
				Cypher(`MATCH (n:LocaleTestNode {id: $id}) SET n += $props`).
				RunWithParams(ctx, map[string]any{"id": "locale-create-1", "props": params})
			require.NoError(t, err)

			props := rawProps(t, ctx, d, "LocaleTestNode", "locale-create-1")
			assert.Equal(t, "Calculus", props["title"], "base should be preserved")
			assert.Equal(t, "Calculus", props["title_enAU"], "locale should be preserved")
		})

		t.Run("read unmarshals preferred locale into base field", func(t *testing.T) {
			// Directly write divergent values via raw session (title != title_enAU)
			session := d.DB().NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
			_, err := session.Run(ctx,
				`MATCH (n:LocaleTestNode {id: $id})
				 SET n.title = 'Base Value', n.title_enAU = 'AU Value'`,
				map[string]any{"id": "locale-create-1"},
			)
			require.NoError(t, err)
			session.Close(ctx)

			// Read back via neogo (unmarshal hooks should fire)
			var node localeTestNode
			err = d.Exec().
				Cypher(`MATCH (n:LocaleTestNode {id: $id})`).
				Return(db.Qual(&node, "n")).
				RunWithParams(ctx, map[string]any{"id": "locale-create-1"})
			require.NoError(t, err)
			assert.Equal(t, "AU Value", node.Title,
				"unmarshal hook should override base with preferred locale")
			require.NotNil(t, node.TitleLocale)
			assert.Equal(t, "AU Value", node.TitleLocale.EnAU)
		})

		t.Run("multi-field: content + figure", func(t *testing.T) {
			q := localeTestQuestion{
				Content: "What is 2+2?",
				Figure:  "https://example.com/fig.png",
			}
			q.ID = "locale-q-1"
			err := d.Exec().
				Cypher(`CREATE (n:LocaleTestQuestion) SET n = {id: $id}, n += $props`).
				Return(db.Qual(&q, "n")).
				RunWithParams(ctx, map[string]any{"id": q.ID, "props": q})
			require.NoError(t, err)

			props := rawProps(t, ctx, d, "LocaleTestQuestion", "locale-q-1")
			assert.Equal(t, "What is 2+2?", props["content"])
			assert.Equal(t, "What is 2+2?", props["content_enAU"])
			assert.Equal(t, "https://example.com/fig.png", props["figure"])
			assert.Equal(t, "https://example.com/fig.png", props["figure_enAU"])
			_, hasContentUS := props["content_enUS"]
			_, hasFigureUS := props["figure_enUS"]
			assert.False(t, hasContentUS)
			assert.False(t, hasFigureUS)
		})

		t.Run("multi-field: update content only preserves figure locale", func(t *testing.T) {
			params := localeTestQuestionUpdate{
				Content: strPtr("What is 3+3?"),
				// Figure is nil — not provided
			}
			err := d.Exec().
				Cypher(`MATCH (n:LocaleTestQuestion {id: $id}) SET n += $props`).
				RunWithParams(ctx, map[string]any{"id": "locale-q-1", "props": params})
			require.NoError(t, err)

			props := rawProps(t, ctx, d, "LocaleTestQuestion", "locale-q-1")
			assert.Equal(t, "What is 3+3?", props["content"])
			assert.Equal(t, "What is 3+3?", props["content_enAU"])
			assert.Equal(t, "https://example.com/fig.png", props["figure"],
				"figure base should be preserved")
			assert.Equal(t, "https://example.com/fig.png", props["figure_enAU"],
				"figure locale should be preserved when not in update")
		})

		t.Run("multi-field: clear figure with empty string", func(t *testing.T) {
			params := localeTestQuestionUpdate{
				Content: strPtr("What is 3+3?"),
				Figure:  strPtr(""),
			}
			err := d.Exec().
				Cypher(`MATCH (n:LocaleTestQuestion {id: $id}) SET n += $props`).
				RunWithParams(ctx, map[string]any{"id": "locale-q-1", "props": params})
			require.NoError(t, err)

			props := rawProps(t, ctx, d, "LocaleTestQuestion", "locale-q-1")
			assert.Equal(t, "", props["figure"])
			assert.Equal(t, "", props["figure_enAU"],
				"clearing figure should write empty string to locale")
			assert.Equal(t, "What is 3+3?", props["content_enAU"],
				"content locale should be unaffected")
		})

		t.Run("read multi-field unmarshals both locale fields", func(t *testing.T) {
			// Write divergent values via raw session
			session := d.DB().NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
			_, err := session.Run(ctx,
				`MATCH (n:LocaleTestQuestion {id: $id})
				 SET n.content = 'base-content', n.content_enAU = 'au-content',
				     n.figure = 'base-fig', n.figure_enAU = 'au-fig'`,
				map[string]any{"id": "locale-q-1"},
			)
			require.NoError(t, err)
			session.Close(ctx)

			var q localeTestQuestion
			err = d.Exec().
				Cypher(`MATCH (n:LocaleTestQuestion {id: $id})`).
				Return(db.Qual(&q, "n")).
				RunWithParams(ctx, map[string]any{"id": "locale-q-1"})
			require.NoError(t, err)
			assert.Equal(t, "au-content", q.Content,
				"content should be overridden by locale")
			assert.Equal(t, "au-fig", q.Figure,
				"figure should be overridden by locale")
		})
	})

	t.Run("US cluster", func(t *testing.T) {
		d := newLocaleDriver(t, ctx, []string{"EnUS", "EnAU"})

		t.Run("create writes base + enUS only", func(t *testing.T) {
			n := localeTestNode{Title: "US Algebra"}
			n.ID = "locale-us-1"
			err := d.Exec().
				Cypher(`CREATE (n:LocaleTestNode) SET n = {id: $id}, n += $props`).
				Return(db.Qual(&n, "n")).
				RunWithParams(ctx, map[string]any{"id": n.ID, "props": n})
			require.NoError(t, err)

			props := rawProps(t, ctx, d, "LocaleTestNode", "locale-us-1")
			assert.Equal(t, "US Algebra", props["title"])
			assert.Equal(t, "US Algebra", props["title_enUS"], "US preferred key should be written")
			_, hasAU := props["title_enAU"]
			assert.False(t, hasAU, "AU key must not exist on US cluster DB")
		})

		t.Run("read unmarshals enUS into base", func(t *testing.T) {
			// Write divergent values
			session := d.DB().NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
			_, err := session.Run(ctx,
				`MATCH (n:LocaleTestNode {id: $id})
				 SET n.title = 'Base', n.title_enUS = 'US Value'`,
				map[string]any{"id": "locale-us-1"},
			)
			require.NoError(t, err)
			session.Close(ctx)

			var node localeTestNode
			err = d.Exec().
				Cypher(`MATCH (n:LocaleTestNode {id: $id})`).
				Return(db.Qual(&node, "n")).
				RunWithParams(ctx, map[string]any{"id": "locale-us-1"})
			require.NoError(t, err)
			assert.Equal(t, "US Value", node.Title,
				"unmarshal should use EnUS as preferred on US cluster")
		})
	})
}
