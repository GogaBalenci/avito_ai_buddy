// Package tracks loads adaptation tracks from YAML (docs/08, «Треки») and
// checks them with the loader rules 1–8 (11 §1.12). A direction is data:
// a new track is a new file, the code knows only the format.
//
// The package is shared: mcp-mocks loads tracks at startup, CI runs the same
// checks on content/tracks/*.yaml.
package tracks

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/GogaBalenci/avito_ai_buddy/content"
	"github.com/GogaBalenci/avito_ai_buddy/internal/yamlx"
)

// Period is a stage of adaptation in days from the start (from common.yaml).
type Period struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	FromDay int    `json:"from_day"`
	ToDay   *int   `json:"to_day"`
}

// Help says what the buddy does on a step.
type Help struct {
	Kind        string `json:"kind"`
	KBDoc       string `json:"kb_doc,omitempty"`
	SourceLevel string `json:"source_level,omitempty"`
	Resource    string `json:"resource,omitempty"`
}

// Step is one item of a track.
type Step struct {
	ID           string `json:"id"`
	ChecklistRef string `json:"checklist_ref,omitempty"`
	Period       string `json:"period"`
	Order        int    `json:"order"`
	Title        string `json:"title"`
	Type         string `json:"type"`
	Owner        string `json:"owner"`
	Help         Help   `json:"help"`
	SyntheticPtr *bool  `json:"synthetic,omitempty"`

	TrackID   string `json:"-"`
	Synthetic bool   `json:"-"` // own flag or the track's
	IsCommon  bool   `json:"-"`
}

// Track is one YAML file.
type Track struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Kind          string   `json:"kind"`
	Extends       string   `json:"extends,omitempty"`
	Synthetic     bool     `json:"synthetic"`
	Disclaimer    string   `json:"disclaimer,omitempty"`
	Source        string   `json:"source"`
	Periods       []Period `json:"periods,omitempty"`
	Steps         []Step   `json:"steps"`
}

// Refs are the ids rule 5 checks against; a nil map skips that check
// (the cross-reference test checks the full content).
type Refs struct {
	KBDocs    map[string]bool
	Resources map[string]bool
}

// Error is a loader rule violation.
type Error struct {
	File string
	Step string
	Rule int
	Msg  string
}

func (e *Error) Error() string {
	s := fmt.Sprintf("tracks/%s: rule %d", e.File, e.Rule)
	if e.Step != "" {
		s += ", step " + e.Step
	}
	return s + ": " + e.Msg
}

// Set is the loaded and checked content of the tracks directory.
type Set struct {
	Periods []Period
	Common  *Track
	tracks  map[string]*Track
}

// Track returns a direction or the common track by id.
func (s *Set) Track(id string) (*Track, bool) { t, ok := s.tracks[id]; return t, ok }

// IDs returns ids of all tracks, sorted.
func (s *Set) IDs() []string {
	ids := make([]string, 0, len(s.tracks))
	for id := range s.tracks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// PeriodIndex returns the position of a period in common.yaml (-1 if unknown).
func (s *Set) PeriodIndex(id string) int {
	for i, p := range s.Periods {
		if p.ID == id {
			return i
		}
	}
	return -1
}

// Path returns the steps of an employee of the given direction:
// common + direction, all owners, in file order.
func (s *Set) Path(trackID string) ([]Step, error) {
	t, ok := s.tracks[trackID]
	if !ok || t.Kind != "direction" {
		return nil, fmt.Errorf("tracks: unknown direction %q", trackID)
	}
	out := make([]Step, 0, len(s.Common.Steps)+len(t.Steps))
	out = append(out, s.Common.Steps...)
	out = append(out, t.Steps...)
	return out, nil
}

var (
	schemaOnce sync.Once
	schema     *jsonschema.Schema
	schemaErr  error
)

func trackSchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		raw, err := fs.ReadFile(content.FS(), "tracks/track.schema.json")
		if err != nil {
			schemaErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			schemaErr = err
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource("https://contracts.buddy.local/tracks/track.schema.json", doc); err != nil {
			schemaErr = err
			return
		}
		schema, schemaErr = c.Compile("https://contracts.buddy.local/tracks/track.schema.json")
	})
	return schema, schemaErr
}

// Load reads every *.yaml in fsys (the tracks directory) and applies rules 1–8.
// Any violation fails the whole load: the service must not start (P-A15).
func Load(fsys fs.FS, refs Refs) (*Set, error) {
	sch, err := trackSchema()
	if err != nil {
		return nil, fmt.Errorf("tracks: schema: %w", err)
	}
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var errs []error
	set := &Set{tracks: map[string]*Track{}}
	for _, name := range names {
		t, err := parse(fsys, name, sch)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		set.tracks[t.ID] = t
		if t.Kind == "common" {
			if set.Common != nil {
				errs = append(errs, &Error{File: name, Rule: 2, Msg: "second common track"})
				continue
			}
			set.Common = t
			set.Periods = t.Periods
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if set.Common == nil {
		return nil, &Error{File: "common.yaml", Rule: 2, Msg: "common track is missing"}
	}
	errs = append(errs, checkCommon(set)...)
	for _, id := range set.IDs() {
		t := set.tracks[id]
		if t.Kind == "direction" {
			errs = append(errs, checkDirection(set, t)...)
		}
		errs = append(errs, checkSteps(t, set, refs)...)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return set, nil
}

func parse(fsys fs.FS, name string, sch *jsonschema.Schema) (*Track, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	j, err := yamlx.ToJSON(raw)
	if err != nil {
		return nil, &Error{File: name, Rule: 1, Msg: err.Error()}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		return nil, &Error{File: name, Rule: 1, Msg: err.Error()}
	}
	if err := sch.Validate(inst); err != nil {
		return nil, &Error{File: name, Rule: 1, Msg: "does not match track.schema.json: " + oneLine(err)}
	}
	var t Track
	if _, err := yamlx.Decode(raw, &t); err != nil {
		return nil, &Error{File: name, Rule: 1, Msg: err.Error()}
	}
	if want := strings.TrimSuffix(path.Base(name), ".yaml"); t.ID != want {
		return nil, &Error{File: name, Rule: 2, Msg: fmt.Sprintf("track id %q does not match file name %q", t.ID, want)}
	}
	for i := range t.Steps {
		st := &t.Steps[i]
		st.TrackID = t.ID
		st.IsCommon = t.Kind == "common"
		st.Synthetic = t.Synthetic
		if st.SyntheticPtr != nil {
			st.Synthetic = *st.SyntheticPtr
		}
	}
	return &t, nil
}

func oneLine(err error) string {
	s := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func checkCommon(set *Set) []error {
	var errs []error
	seen := map[string]bool{}
	prevTo := 0
	for _, p := range set.Periods {
		if seen[p.ID] {
			errs = append(errs, &Error{File: "common.yaml", Rule: 4, Msg: "duplicate period " + p.ID})
		}
		seen[p.ID] = true
		if p.FromDay != prevTo+1 {
			errs = append(errs, &Error{File: "common.yaml", Rule: 4, Msg: fmt.Sprintf("period %s must start on day %d", p.ID, prevTo+1)})
		}
		if p.ToDay == nil {
			prevTo = 1 << 30
		} else {
			if *p.ToDay < p.FromDay {
				errs = append(errs, &Error{File: "common.yaml", Rule: 4, Msg: "period " + p.ID + " ends before it starts"})
			}
			prevTo = *p.ToDay
		}
	}
	return errs
}

func checkDirection(set *Set, t *Track) []error {
	var errs []error
	file := t.ID + ".yaml"
	if t.Extends != "common" {
		errs = append(errs, &Error{File: file, Rule: 2, Msg: "direction must have extends: common"})
	}
	// rule 3: ids unique in common + direction
	common := map[string]bool{}
	for _, s := range set.Common.Steps {
		common[s.ID] = true
	}
	for _, s := range t.Steps {
		if common[s.ID] {
			errs = append(errs, &Error{File: file, Step: s.ID, Rule: 3, Msg: "step id duplicates a common step"})
		}
	}
	return errs
}

func checkSteps(t *Track, set *Set, refs Refs) []error {
	var errs []error
	file := t.ID + ".yaml"
	ids := map[string]bool{}
	slots := map[string]string{}
	if t.Synthetic && strings.TrimSpace(t.Disclaimer) == "" {
		errs = append(errs, &Error{File: file, Rule: 6, Msg: "synthetic track without disclaimer"})
	}
	for _, field := range []string{t.Title, t.Disclaimer, t.Source} {
		if n := content.FindBannedName(field); n != "" {
			errs = append(errs, &Error{File: file, Rule: 7, Msg: "real person's name in track text: " + n})
		}
	}
	for _, s := range t.Steps {
		if ids[s.ID] {
			errs = append(errs, &Error{File: file, Step: s.ID, Rule: 3, Msg: "duplicate step id"})
		}
		ids[s.ID] = true
		if set.PeriodIndex(s.Period) < 0 {
			errs = append(errs, &Error{File: file, Step: s.ID, Rule: 4, Msg: "unknown period " + s.Period})
		}
		if refs.KBDocs != nil && s.Help.KBDoc != "" && !refs.KBDocs[s.Help.KBDoc] {
			errs = append(errs, &Error{File: file, Step: s.ID, Rule: 5, Msg: "kb_doc " + s.Help.KBDoc + " is not in the knowledge base"})
		}
		if refs.Resources != nil && s.Help.Resource != "" && !refs.Resources[s.Help.Resource] {
			errs = append(errs, &Error{File: file, Step: s.ID, Rule: 5, Msg: "resource " + s.Help.Resource + " is not in the access catalog"})
		}
		if n := content.FindBannedName(s.Title); n != "" {
			errs = append(errs, &Error{File: file, Step: s.ID, Rule: 7, Msg: "real person's name in step title: " + n})
		}
		slot := fmt.Sprintf("%s/%d", s.Period, s.Order)
		if other, dup := slots[slot]; dup {
			errs = append(errs, &Error{File: file, Step: s.ID, Rule: 8, Msg: fmt.Sprintf("(period, order) %s repeats step %s in the same file", slot, other)})
		}
		slots[slot] = s.ID
	}
	return errs
}
