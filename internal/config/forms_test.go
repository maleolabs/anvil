package config

import (
	"encoding/json"
	"testing"
)

func intPtr(i int) *int { return &i }

func TestValidateFormsSchema_SixTypes(t *testing.T) {
	forms := InstallerForms{
		"superAdmin": {
			Fields: []FormField{
				{Name: "username", Type: "text", Required: true},
				{Name: "email", Type: "email", Required: true, Pattern: `^[^@]+@[^@]+\.[^@]+$`},
				{Name: "password", Type: "password", Required: true, MinLength: intPtr(8), Confirmation: "password_confirmation"},
				{Name: "password_confirmation", Type: "password", Required: true},
				{Name: "role", Type: "select", Options: []string{"admin", "user"}},
				{Name: "age", Type: "number", Required: false},
				{Name: "bio", Type: "textarea", Required: false},
			},
		},
	}
	errs := ValidateFormsSchema(forms)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for 6 types, got %v", errs)
	}
	// also test MarshalFormsJSON
	b, err := MarshalFormsJSON(forms)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded InstallerForms
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded["superAdmin"].Fields) != 7 {
		t.Fatalf("decoded fields count want 7 got %d", len(decoded["superAdmin"].Fields))
	}
}

func TestValidateFormsSchema_ValidationFailures(t *testing.T) {
	// duplicate name
	formsDup := InstallerForms{
		"form1": {Fields: []FormField{{Name: "email", Type: "email"}, {Name: "email", Type: "text"}}},
	}
	errs := ValidateFormsSchema(formsDup)
	if len(errs) == 0 {
		t.Fatalf("expected duplicate name error")
	}

	// invalid type
	formsType := InstallerForms{
		"form1": {Fields: []FormField{{Name: "f1", Type: "unknown"}}},
	}
	if len(ValidateFormsSchema(formsType)) == 0 {
		t.Fatalf("expected invalid type error")
	}

	// invalid pattern
	formsPat := InstallerForms{
		"form1": {Fields: []FormField{{Name: "f1", Type: "text", Pattern: "["}}},
	}
	if len(ValidateFormsSchema(formsPat)) == 0 {
		t.Fatalf("expected invalid pattern error")
	}

	// confirmation referencing non-existent
	formsConf := InstallerForms{
		"form1": {Fields: []FormField{{Name: "pwd", Type: "password", Confirmation: "pwd2"}}},
	}
	if len(ValidateFormsSchema(formsConf)) == 0 {
		t.Fatalf("expected confirmation error")
	}

	// minLength negative
	formsMin := InstallerForms{
		"form1": {Fields: []FormField{{Name: "f1", Type: "text", MinLength: intPtr(-1)}}},
	}
	if len(ValidateFormsSchema(formsMin)) == 0 {
		t.Fatalf("expected minLength error")
	}

	// when referencing non-existent field
	formsWhen := InstallerForms{
		"form1": {Fields: []FormField{{Name: "a", Type: "text"}, {Name: "b", Type: "text", When: &WhenCondition{Field: "nonexistent", Value: "x"}}}},
	}
	if len(ValidateFormsSchema(formsWhen)) == 0 {
		t.Fatalf("expected when field error")
	}

	// select without options
	formsSel := InstallerForms{
		"form1": {Fields: []FormField{{Name: "role", Type: "select"}}},
	}
	if len(ValidateFormsSchema(formsSel)) == 0 {
		t.Fatalf("expected select options error")
	}

	// name regex failure
	formsName := InstallerForms{
		"form1": {Fields: []FormField{{Name: "1bad", Type: "text"}}},
	}
	if len(ValidateFormsSchema(formsName)) == 0 {
		t.Fatalf("expected name regex error")
	}
}

func TestValidateFormsSchema_ConditionalWhen(t *testing.T) {
	forms := InstallerForms{
		"profile": {
			Fields: []FormField{
				{Name: "hasCompany", Type: "select", Options: []string{"yes", "no"}},
				{Name: "companyName", Type: "text", When: &WhenCondition{Field: "hasCompany", Value: "yes"}},
			},
		},
	}
	errs := ValidateFormsSchema(forms)
	if len(errs) != 0 {
		t.Fatalf("when conditional should pass, got %v", errs)
	}
	// invalid when referencing missing field
	forms2 := InstallerForms{
		"profile": {
			Fields: []FormField{
				{Name: "companyName", Type: "text", When: &WhenCondition{Field: "missing", Value: "yes"}},
			},
		},
	}
	if len(ValidateFormsSchema(forms2)) == 0 {
		t.Fatalf("expected when reference error")
	}
}

func TestParseInstallerFormsFromFlat(t *testing.T) {
	flat := map[string]interface{}{
		"installer.forms": map[string]interface{}{
			"superAdmin": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "email", "type": "email", "required": true},
					map[string]interface{}{"name": "password", "type": "password", "required": true},
				},
			},
		},
	}
	forms, err := ParseInstallerFormsFromFlat(flat)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(forms["superAdmin"].Fields) != 2 {
		t.Fatalf("fields count")
	}
	// also test fragmented keys fallback
	flat2 := map[string]interface{}{
		"installer.forms.superAdmin.fields": []interface{}{
			map[string]interface{}{"name": "email", "type": "email"},
		},
	}
	forms2, err := ParseInstallerFormsFromFlat(flat2)
	if err != nil {
		t.Fatalf("parse2: %v", err)
	}
	if forms2 == nil || len(forms2["superAdmin"].Fields) != 1 {
		t.Fatalf("fragmented parse failed: %v", forms2)
	}
}

func TestPasswordFieldNames(t *testing.T) {
	forms := InstallerForms{
		"a": {Fields: []FormField{{Name: "pwd", Type: "password"}, {Name: "email", Type: "email"}, {Name: "secret", Type: "password"}}},
	}
	names := PasswordFieldNames(forms)
	if len(names) != 2 {
		t.Fatalf("want 2 password fields got %v", names)
	}
}
