package router

import (
	"context"
	"errors"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/kb"
)

// Doc is the part of a knowledge-base document a reply needs.
type Doc struct {
	Slug        string
	Title       string
	Label       string
	SourceLevel string
	Dated       *string
	Summary     string
}

// ErrDocNotFound: no such document or it is closed to the role
// (a closed document does not exist for get_doc, 11 §2.6.3).
var ErrDocNotFound = errors.New("document not found")

// DocSource reads one document for the actor (knowledge.get_doc).
type DocSource interface {
	Doc(ctx context.Context, a actor.Actor, slug string) (Doc, error)
}

// StaticDocs serves documents from the loaded content with the role filter.
// It is the demo-mode source until the knowledge endpoint (S2) is connected;
// the same interface is then backed by knowledge.get_doc.
type StaticDocs map[string]kb.Doc

// NewStaticDocs indexes documents by slug.
func NewStaticDocs(docs []kb.Doc) StaticDocs {
	m := StaticDocs{}
	for _, d := range docs {
		m[d.Slug] = d
	}
	return m
}

// Doc implements DocSource.
func (s StaticDocs) Doc(_ context.Context, a actor.Actor, slug string) (Doc, error) {
	d, ok := s[slug]
	if !ok || !d.AllowedFor(a.Role) {
		return Doc{}, ErrDocNotFound
	}
	return Doc{Slug: d.Slug, Title: d.Title, Label: d.Label(), SourceLevel: d.SourceLevel, Dated: d.Dated, Summary: d.Summary}, nil
}
