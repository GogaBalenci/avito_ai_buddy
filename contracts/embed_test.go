package contracts

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAllSchemasCompile(t *testing.T) {
	if err := Check(); err != nil {
		t.Fatal(err)
	}
	if n := len(Names()); n != 29 {
		t.Fatalf("schemas = %d", n)
	}
}

func TestValidate(t *testing.T) {
	ok := map[string]any{"actor": map[string]any{"user_id": "u-intern-be", "role": "intern"},
		"idempotency_key": "6f1c1c55-7c8e-4a51-9d35-3e3a8d3c1a10", "params_hash": strings.Repeat("a", 64),
		"type": "time_off", "date_from": "2026-11-13", "date_to": "2026-11-13"}
	if err := Validate("absences/create_request.input.v1.json", ok); err != nil {
		t.Fatal(err)
	}
	ok["reason"] = "болею" // M-07: extra field
	err := Validate("absences/create_request.input.v1.json", ok)
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Errors) == 0 || !strings.Contains(err.Error(), "create_request") {
		t.Fatalf("extra field: %v", err)
	}
	// actor: system must be svc-
	sys := map[string]any{"actor": map[string]any{"user_id": "u-x", "role": "system"}}
	if err := Validate("access/list_resources.input.v1.json", sys); err == nil {
		t.Fatal("system actor with u- id accepted")
	}
	bad := map[string]any{"actor": map[string]any{"user_id": "u-intern-be", "role": "intern"},
		"idempotency_key": "not-a-uuid", "params_hash": strings.Repeat("a", 64),
		"type": "time_off", "date_from": "2026-02-30", "date_to": "2026-11-13"}
	if err := Validate("absences/create_request.input.v1.json", bad); err == nil {
		t.Fatal("format assertions are off")
	}
	if err := ValidateJSON("nope.json", []byte(`{}`)); !errors.Is(err, ErrUnknownSchema) {
		t.Fatalf("unknown: %v", err)
	}
	if err := ValidateJSON("common/actor.v1.json", []byte(`{`)); err == nil {
		t.Fatal("bad JSON accepted")
	}
	if err := Validate("common/actor.v1.json", func() {}); err == nil {
		t.Fatal("unmarshalable accepted")
	}
}

func TestBundleInlinesRefs(t *testing.T) {
	for _, n := range Names() {
		b, err := Bundle(n)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		raw, _ := json.Marshal(b)
		if strings.Contains(string(raw), `"$ref"`) {
			t.Fatalf("%s still has $ref", n)
		}
	}
	b, _ := Bundle("absences/create_request.output.v1.json")
	req := b["properties"].(map[string]any)["request"].(map[string]any)
	if req["title"] != "AbsenceRequest" {
		t.Fatalf("request not inlined: %v", req)
	}
	if _, err := Bundle("missing.json"); err == nil {
		t.Fatal("missing accepted")
	}
	if FS() == nil {
		t.Fatal("FS")
	}
}
