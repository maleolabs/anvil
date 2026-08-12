package registry

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// sampleTemplateContent returns template content for the laravel
// namespace exercising every declared field of the TS-015-02-03 template
// content shape: a build template file and a CI template file, each
// carrying its pipeline file content in the pipeline file format (YAML —
// the shape the runtime validates through the pipeline loader before
// writing).
func sampleTemplateContent() *TemplateContent {
	return &TemplateContent{
		Namespace: "laravel",
		Templates: []TemplateFile{
			{ID: "build", Description: "Laravel build pipeline.", Content: sampleBuildYAML()},
			{ID: "ci", Description: "Laravel CI pipeline.", Content: sampleCIYAML()},
		},
	}
}

// sampleBuildYAML returns the pipeline file content of the build
// template: the Laravel build definition in the pipeline file format
// (the same definition the Laravel adapter's template command returns —
// parity fixture, TS-015-02-03).
func sampleBuildYAML() string {
	return `pipeline:
  name: build
  stages:
    - name: dependencies
      tasks:
        - name: composer-install
          command: composer
          args: [install, --no-dev, --optimize-autoloader]
    - name: assets
      tasks:
        - name: npm-build
          command: npm
          args: [run, build]
    - name: optimize
      tasks:
        - name: cache-config
          command: php
          args: [artisan, config:cache]
`
}

// sampleCIYAML returns the pipeline file content of the CI template.
func sampleCIYAML() string {
	return `pipeline:
  name: ci
  stages:
    - name: test
      tasks:
        - name: unit-tests
          command: echo
          args: [ok]
`
}

// TestInstalledStandardRecord_TemplateContentRoundTrip verifies that the
// template content is part of the installed standard (TS-015-02-03): a
// record written with embedded content reads back with the content
// byte-for-byte — the store persists the standard's content and never
// drops or interprets it (mirroring the embedded Compatibility/Trust and
// ConfigExtension patterns).
func TestInstalledStandardRecord_TemplateContentRoundTrip(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.Templates = sampleTemplateContent()
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := store.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got.Templates, sampleTemplateContent()) {
		t.Errorf("Get returned Templates %+v, want %+v", got.Templates, sampleTemplateContent())
	}
}

// TestInstalledStandardRecord_TemplateContentAbsent verifies that a
// record written without template content reads back without it (null):
// a standard that declares nothing in the template category
// (command-contract §4.1) leaves the embedded section absent — the
// resolution hands off to ErrTemplateContentMissing and generation falls
// back to the interim adapter-driven path (TS-015-02-03).
func TestInstalledStandardRecord_TemplateContentAbsent(t *testing.T) {
	store := newTestStore(t)
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	if _, _, err := store.Record(rec.ID, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := store.Get(rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Templates != nil {
		t.Errorf("Get returned Templates %+v, want nil", got.Templates)
	}
}

// TestTemplateContent_Resolved verifies the record-level resolution
// success case (TS-015-02-03 DoD: generated content comes from the
// installed standard): a record carrying content whose namespace matches
// the declared framework resolves to exactly that content — the pipeline
// templates come from the installed standard, never from runtime
// knowledge.
func TestTemplateContent_Resolved(t *testing.T) {
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.Templates = sampleTemplateContent()

	got, err := rec.TemplateContent("laravel")
	if err != nil {
		t.Fatalf("TemplateContent: %v", err)
	}
	if !reflect.DeepEqual(got, *sampleTemplateContent()) {
		t.Errorf("TemplateContent returned %+v, want %+v", got, *sampleTemplateContent())
	}
}

// TestTemplateContent_Missing verifies the no-content outcome
// (TS-015-02-03): a resolved standard whose record carries no template
// content yields the wrapped ErrTemplateContentMissing hand-off signal —
// a distinguishable outcome, never a store failure and never an invented
// resolution. The caller decides how it is surfaced (warning pattern,
// mirroring T-004's ErrStandardNotInstalled / ErrConfigExtensionMissing
// hand-offs); the interim adapter-driven generation covers the gap.
func TestTemplateContent_Missing(t *testing.T) {
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")

	_, err := rec.TemplateContent("laravel")
	if !errors.Is(err, ErrTemplateContentMissing) {
		t.Fatalf("TemplateContent error = %v, want wrapped %v", err, ErrTemplateContentMissing)
	}
	if !strings.Contains(err.Error(), rec.ID) {
		t.Errorf("missing-content error should name the standard, got: %v", err)
	}
}

// TestTemplateContent_EmptyTemplatesList_Missing verifies that a content
// section declaring an empty templates list resolves as no-content: the
// section carries nothing to generate from, indistinguishable from no
// section for generation purposes (command-contract §4.1 — a standard
// may declare nothing in a category) — the hand-off signal is the same
// ErrTemplateContentMissing.
func TestTemplateContent_EmptyTemplatesList_Missing(t *testing.T) {
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.Templates = &TemplateContent{Namespace: "laravel"}

	_, err := rec.TemplateContent("laravel")
	if !errors.Is(err, ErrTemplateContentMissing) {
		t.Fatalf("TemplateContent error = %v, want wrapped %v", err, ErrTemplateContentMissing)
	}
}

// TestTemplateContent_NamespaceMismatch verifies the namespace isolation
// guard (TS-015-02-03, C6 / command-contract §4.5): content declaring a
// namespace different from the declared framework is a violation — the
// record is inconsistent with the standard it belongs to, an actionable
// error with the reinstall remediation, never a silent pass-through of
// foreign content.
func TestTemplateContent_NamespaceMismatch(t *testing.T) {
	rec := sampleRecord("anvil-standard-laravel", "1.2.3")
	rec.Templates = &TemplateContent{
		Namespace: "rails",
		Templates: []TemplateFile{
			{ID: "build", Content: "pipeline:\n  name: build\n"},
		},
	}

	_, err := rec.TemplateContent("laravel")
	if err == nil {
		t.Fatal("TemplateContent expected a namespace violation error, got nil")
	}
	for _, want := range []string{"rails", "laravel", "re-install"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("namespace violation error should mention %q, got: %v", want, err)
		}
	}
}
