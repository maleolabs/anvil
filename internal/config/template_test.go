package config

import "testing"

func TestResolveFormsTemplate(t *testing.T) {
	values := map[string]map[string]string{
		"superAdmin": {"email": "admin@example.com", "name": "Alice"},
	}
	got := ResolveFormsTemplate("hello {{forms.superAdmin.email}}", values)
	if got != "hello admin@example.com" {
		t.Fatalf("got %q", got)
	}
	got = ResolveFormsTemplate("{{forms.superAdmin.name}} is {{forms.superAdmin.email}}", values)
	if got != "Alice is admin@example.com" {
		t.Fatalf("got %q", got)
	}
	// missing field leaves placeholder
	got = ResolveFormsTemplate("{{forms.superAdmin.missing}}", values)
	if got != "{{forms.superAdmin.missing}}" {
		t.Fatalf("missing should leave placeholder, got %q", got)
	}
}

func TestResolveFormsTemplateWithFallback(t *testing.T) {
	values := map[string]map[string]string{
		"superAdmin": {"email": "from@form.com"},
	}
	// resolved case
	got := ResolveFormsTemplateWithFallback("{{forms.superAdmin.email}}", values, "fallback@example.com")
	if got != "from@form.com" {
		t.Fatalf("fallback resolved: %q", got)
	}
	// unresolved with fallback
	valuesEmpty := map[string]map[string]string{}
	got = ResolveFormsTemplateWithFallback("{{forms.superAdmin.email}}", valuesEmpty, "hardcode@example.com")
	if got != "hardcode@example.com" {
		t.Fatalf("fallback unresolved: %q", got)
	}
	// no placeholder returns tmpl unchanged
	got = ResolveFormsTemplateWithFallback("plain text", valuesEmpty, "fallback@example.com")
	if got != "plain text" {
		t.Fatalf("plain: %q", got)
	}
	// superAdmin identifier templated from form input (fallback hardcode)
	templateStr := "{{forms.superAdmin.email}}"
	formValues := map[string]map[string]string{"superAdmin": {"email": "user@test.com"}}
	if ResolveFormsTemplateWithFallback(templateStr, formValues, "admin@example.com") != "user@test.com" {
		t.Fatalf("template resolution failed")
	}
	if ResolveFormsTemplateWithFallback(templateStr, map[string]map[string]string{}, "admin@example.com") != "admin@example.com" {
		t.Fatalf("fallback failed")
	}
}
