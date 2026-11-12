// Package knowledge is the mock of the corporate knowledge base, endpoint
// /mcp/knowledge (11 §2.10): PostgreSQL full-text search with the rights in
// SQL — the body of a closed document is never selected (AG-06).
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GogaBalenci/avito_ai_buddy/internal/kb"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpmocks/mcpx"
)

// Service is the knowledge mock.
type Service struct {
	Pool *pgxpool.Pool
}

// Register adds the tools.
func (s *Service) Register(srv *mcp.Server, log *slog.Logger) {
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "knowledge", Name: "search", ReadOnly: true,
		Description: "Полнотекстовый поиск по тестовой базе знаний с учётом прав роли. Возвращает краткие выдержки и заголовки недоступных документов без содержимого.",
		Handler:     s.search})
	mcpx.Add(srv, log, mcpx.Tool{Endpoint: "knowledge", Name: "get_doc", ReadOnly: true,
		Description: "Документ тестовой базы знаний по slug с учётом прав роли.",
		Handler:     s.getDoc})
}

// LoadContent writes the documents and computes the search vectors (11 §2.5.3).
func LoadContent(ctx context.Context, tx pgx.Tx, docs []kb.Doc, now time.Time) error {
	slugs := make([]string, 0, len(docs))
	for _, d := range docs {
		slugs = append(slugs, d.Slug)
		if _, err := tx.Exec(ctx, `INSERT INTO mock_knowledge.docs (slug, title, kind, source_level, dated, allowed_roles, disclose_title,
			tracks, periods, tags, conflicts_with, test_only, summary, body_md, tsv, title_tsv, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,
			  setweight(to_tsvector('russian', $2), 'A') || setweight(to_tsvector('russian', array_to_string($10::text[], ' ')), 'B')
			    || setweight(to_tsvector('russian', $14), 'C'),
			  to_tsvector('russian', $2 || ' ' || array_to_string($10::text[], ' ')), $15)
			ON CONFLICT (slug) DO UPDATE SET title = EXCLUDED.title, kind = EXCLUDED.kind, source_level = EXCLUDED.source_level,
			  dated = EXCLUDED.dated, allowed_roles = EXCLUDED.allowed_roles, disclose_title = EXCLUDED.disclose_title,
			  tracks = EXCLUDED.tracks, periods = EXCLUDED.periods, tags = EXCLUDED.tags, conflicts_with = EXCLUDED.conflicts_with,
			  test_only = EXCLUDED.test_only, summary = EXCLUDED.summary, body_md = EXCLUDED.body_md, tsv = EXCLUDED.tsv,
			  title_tsv = EXCLUDED.title_tsv`,
			d.Slug, d.Title, d.Kind, d.SourceLevel, d.Dated, nn(d.AllowedRoles), d.DiscloseTitle, nn(d.Tracks), nn(d.Periods),
			nn(d.Tags), nn(d.ConflictsWith), d.TestOnly, d.Summary, d.Body, now); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `DELETE FROM mock_knowledge.docs WHERE NOT slug = ANY($1)`, slugs)
	return err
}

func nn(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func label(sourceLevel string) string {
	if sourceLevel == "none" {
		return "test_description"
	}
	return "test_instruction"
}

// Item is an item of search.output.
type Item struct {
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Kind          string   `json:"kind"`
	Summary       string   `json:"summary"`
	Snippet       string   `json:"snippet"`
	Rank          float64  `json:"rank"`
	Label         string   `json:"label"`
	SourceLevel   string   `json:"source_level"`
	Dated         *string  `json:"dated"`
	URL           string   `json:"url"`
	ConflictsWith []string `json:"conflicts_with"`
}

// Denied is a closed document: slug and title only.
type Denied struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// Lexemes of the query, joined by OR (11 §2.6.1, p. 3): plainto_tsquery
// would join them by AND and lose "сколько ждать код-ревью".
const lexemesSQL = `SELECT coalesce(array_agg(DISTINCT lex ORDER BY lex), '{}')
	FROM unnest(tsvector_to_array(to_tsvector('russian', $1))) AS lex`

// AllowedSQL selects allowed documents; the rights are a WHERE condition (KM-10).
const AllowedSQL = `SELECT d.slug, d.title, d.kind, d.summary, d.source_level, d.dated, d.conflicts_with,
	ts_rank_cd(d.tsv, q.tsq, 32) AS rank,
	ts_headline('russian', d.body_md, q.tsq, 'MaxWords=30, MinWords=10, StartSel="", StopSel=""') AS snippet
FROM mock_knowledge.docs d, (SELECT $1::tsquery AS tsq) q
WHERE d.tsv @@ q.tsq
  AND d.allowed_roles @> ARRAY[$2]::text[]
ORDER BY ts_rank_cd(d.tsv, q.tsq, 32)
         * (1 + 0.3 * (d.tracks @> ARRAY[$3]::text[])::int
              + 0.2 * (d.periods @> ARRAY[$4]::text[])::int) DESC, d.slug
LIMIT $5`

// DeniedSQL selects only slug and title of closed documents with
// disclose_title, and how many query lexemes their title and tags cover.
const DeniedSQL = `SELECT d.slug, d.title,
  (SELECT count(*) FROM unnest($3::text[]) AS l WHERE d.title_tsv @@ quote_literal(l)::tsquery) AS covered
FROM mock_knowledge.docs d
WHERE d.title_tsv @@ $1::tsquery
  AND NOT d.allowed_roles @> ARRAY[$2]::text[]
  AND d.disclose_title
ORDER BY d.slug
LIMIT 3`

// coverSQL counts the query lexemes an allowed document covers.
const coverSQL = `SELECT count(*) FROM mock_knowledge.docs d, unnest($2::text[]) AS l
WHERE d.slug = $1 AND d.tsv @@ quote_literal(l)::tsquery`

func tsquery(lexemes []string) string {
	parts := make([]string, len(lexemes))
	for i, l := range lexemes {
		parts[i] = "'" + strings.ReplaceAll(l, "'", "''") + "'"
	}
	return strings.Join(parts, " | ")
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func (s *Service) search(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor   mcpx.Actor `json:"actor"`
		Query   string     `json:"query"`
		Context struct {
			Track  string `json:"track"`
			Period string `json:"period"`
		} `json:"context"`
		Limit int `json:"limit"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	if in.Actor.Role != "intern" && in.Actor.Role != "mentor" {
		return nil, mcpx.Denied("role is not allowed")
	}
	if in.Limit == 0 {
		in.Limit = 3
	}
	var lex []string
	if err := s.Pool.QueryRow(ctx, lexemesSQL, in.Query).Scan(&lex); err != nil {
		return nil, err
	}
	out := map[string]any{"items": []Item{}, "denied": []Denied{}, "query_lexemes": lex}
	if len(lex) == 0 {
		return out, nil
	}
	tsq := tsquery(lex)
	rows, err := s.Pool.Query(ctx, AllowedSQL, tsq, in.Actor.Role, in.Context.Track, in.Context.Period, in.Limit)
	if err != nil {
		return nil, err
	}
	items := []Item{}
	for rows.Next() {
		var it Item
		var rank float32
		if err := rows.Scan(&it.Slug, &it.Title, &it.Kind, &it.Summary, &it.SourceLevel, &it.Dated, &it.ConflictsWith, &rank, &it.Snippet); err != nil {
			rows.Close()
			return nil, err
		}
		it.Rank, it.Label, it.URL, it.Snippet = float64(rank), label(it.SourceLevel), "/kb/"+it.Slug, cut(it.Snippet, 600)
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// A closed document is reported only when it covers more of the query
	// than the best allowed one: «что в методичке наставника» names the
	// guide, «кто мой наставник» does not.
	best := 0
	if len(items) > 0 {
		if err := s.Pool.QueryRow(ctx, coverSQL, items[0].Slug, lex).Scan(&best); err != nil {
			return nil, err
		}
	}
	denied := []Denied{}
	drows, err := s.Pool.Query(ctx, DeniedSQL, tsq, in.Actor.Role, lex)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var d Denied
		var covered int
		if err := drows.Scan(&d.Slug, &d.Title, &covered); err != nil {
			return nil, err
		}
		if covered > best {
			denied = append(denied, d)
		}
	}
	out["items"], out["denied"] = items, denied
	return out, drows.Err()
}

// Doc is get_doc.output.doc.
type Doc struct {
	Slug          string    `json:"slug"`
	Title         string    `json:"title"`
	Kind          string    `json:"kind"`
	Summary       string    `json:"summary"`
	BodyMD        string    `json:"body_md"`
	Label         string    `json:"label"`
	SourceLevel   string    `json:"source_level"`
	Dated         *string   `json:"dated"`
	URL           string    `json:"url"`
	UpdatedAt     time.Time `json:"updated_at"`
	ConflictsWith []string  `json:"conflicts_with"`
}

func (s *Service) getDoc(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Actor mcpx.Actor `json:"actor"`
		Slug  string     `json:"slug"`
	}
	if err := mcpx.Decode(raw, &in); err != nil {
		return nil, err
	}
	if in.Actor.Role != "intern" && in.Actor.Role != "mentor" {
		return nil, mcpx.Denied("role is not allowed")
	}
	var allowed, disclose bool
	var title string
	err := s.Pool.QueryRow(ctx, `SELECT allowed_roles @> ARRAY[$2]::text[], disclose_title, title FROM mock_knowledge.docs WHERE slug = $1`,
		in.Slug, in.Actor.Role).Scan(&allowed, &disclose, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, mcpx.NotFound("no such document")
	}
	if err != nil {
		return nil, err
	}
	if !allowed {
		if disclose {
			return nil, mcpx.Err("PERMISSION_DENIED", "the document is closed for the role", map[string]any{"title": title})
		}
		return nil, mcpx.NotFound("no such document") // existence is not revealed (K-A3)
	}
	var d Doc
	if err := s.Pool.QueryRow(ctx, `SELECT slug, title, kind, summary, body_md, source_level, dated, updated_at, conflicts_with
		FROM mock_knowledge.docs WHERE slug = $1 AND allowed_roles @> ARRAY[$2]::text[]`, in.Slug, in.Actor.Role).
		Scan(&d.Slug, &d.Title, &d.Kind, &d.Summary, &d.BodyMD, &d.SourceLevel, &d.Dated, &d.UpdatedAt, &d.ConflictsWith); err != nil {
		return nil, err
	}
	d.Label, d.URL = label(d.SourceLevel), "/kb/"+d.Slug
	return map[string]any{"doc": d}, nil
}
