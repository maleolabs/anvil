package skillbundle

import (
	"errors"
	"strings"
	"testing"
)

// validSkillMD returns a SKILL.md with a valid portable frontmatter.
func validSkillMD() string {
	return `---
name: anvil-cli-usage
description: How to use the Anvil CLI
---
# Anvil CLI Usage

Content here.
`
}

// wantFrontmatterError asserts that ParseFrontmatter rejects the document
// with a problem on the given field, and that errors.As reaches a
// *FrontmatterError.
func wantFrontmatterError(t *testing.T, doc, field, messageSubstr string) {
	t.Helper()
	_, err := ParseFrontmatter([]byte(doc))
	if err == nil {
		t.Fatalf("ParseFrontmatter accepted the document; want a rejection (field %q)", field)
	}
	var fe *FrontmatterError
	if !errors.As(err, &fe) {
		t.Fatalf("errors.As(*FrontmatterError) failed; got %T: %v", err, err)
	}
	for _, ve := range fe.Errors {
		if ve.Field == field {
			if messageSubstr != "" && !strings.Contains(ve.Message, messageSubstr) {
				t.Fatalf("field %q problem message %q does not contain %q", field, ve.Message, messageSubstr)
			}
			return
		}
	}
	t.Fatalf("no rejection on field %q; got %v", field, fe.Error())
}

func TestParseFrontmatter_Valid(t *testing.T) {
	fm, err := ParseFrontmatter([]byte(validSkillMD()))
	if err != nil {
		t.Fatalf("ParseFrontmatter rejected a valid SKILL.md: %v", err)
	}
	if fm.Name != "anvil-cli-usage" || fm.Description == "" {
		t.Fatalf("frontmatter mismatch: %+v", fm)
	}
}

func TestParseFrontmatter_AllPortableFields(t *testing.T) {
	doc := `---
name: anvil-cli-usage
description: How to use the Anvil CLI
license: MIT
compatibility:
  opencode: "*"
metadata:
  anvil: true
allowed-tools:
  - Read
  - Grep
---
content
`
	fm, err := ParseFrontmatter([]byte(doc))
	if err != nil {
		t.Fatalf("ParseFrontmatter rejected portable fields: %v", err)
	}
	if fm.License != "MIT" || len(fm.AllowedTools) != 2 || fm.AllowedTools[0] != "Read" {
		t.Fatalf("frontmatter mismatch: %+v", fm)
	}
	if _, ok := fm.Raw["compatibility"]; !ok {
		t.Fatalf("Raw missing compatibility: %+v", fm.Raw)
	}
}

func TestParseFrontmatter_AgentSpecificFieldsRejected(t *testing.T) {
	// Claude Code's "context" field is agent-specific and must be
	// rejected so one artifact stays portable (ADR-037 D1).
	doc := `---
name: anvil-cli-usage
description: How to use the Anvil CLI
context:
  fork: true
---
content
`
	wantFrontmatterError(t, doc, "context", "not a portable Agent Skills field")

	// A literal "source" field is not portable either — provenance is
	// injected at install time, never authored (ADR-037 D10).
	doc = `---
name: anvil-cli-usage
description: How to use the Anvil CLI
source: anvil 1.0.0
---
content
`
	wantFrontmatterError(t, doc, "source", "not a portable Agent Skills field")
}

func TestParseFrontmatter_RequiredFields(t *testing.T) {
	doc := "---\ndescription: no name here\n---\ncontent\n"
	wantFrontmatterError(t, doc, "name", "required field is missing")

	doc = "---\nname: anvil-cli-usage\n---\ncontent\n"
	wantFrontmatterError(t, doc, "description", "required field is missing")
}

func TestParseFrontmatter_NameRules(t *testing.T) {
	// Wrong pattern.
	doc := "---\nname: Bad_Name\ndescription: x\n---\n"
	wantFrontmatterError(t, doc, "name", "skill name convention")
	// Empty.
	doc = "---\nname: \"\"\ndescription: x\n---\n"
	wantFrontmatterError(t, doc, "name", "must not be empty")
	// Wrong type.
	doc = "---\nname: 123\ndescription: x\n---\n"
	wantFrontmatterError(t, doc, "name", "must be a string")
}

func TestParseFrontmatter_TypeChecks(t *testing.T) {
	// description must be a string.
	doc := "---\nname: anvil-cli-usage\ndescription:\n  - not\n  - a\n  - string\n---\n"
	wantFrontmatterError(t, doc, "description", "must be a string")
	// license must be a string.
	doc = "---\nname: anvil-cli-usage\ndescription: x\nlicense: [MIT, Apache]\n---\n"
	wantFrontmatterError(t, doc, "license", "must be a string")
	// allowed-tools must be a sequence of strings.
	doc = "---\nname: anvil-cli-usage\ndescription: x\nallowed-tools: not-a-list\n---\n"
	wantFrontmatterError(t, doc, "allowed-tools", "must be a sequence of strings")
	// compatibility must be a mapping.
	doc = "---\nname: anvil-cli-usage\ndescription: x\ncompatibility: 42\n---\n"
	wantFrontmatterError(t, doc, "compatibility", "must be a mapping")
	// metadata must be a mapping.
	doc = "---\nname: anvil-cli-usage\ndescription: x\nmetadata: [a]\n---\n"
	wantFrontmatterError(t, doc, "metadata", "must be a mapping")
}

func TestParseFrontmatter_BlockShape(t *testing.T) {
	// No opening delimiter.
	wantFrontmatterError(t, "name: anvil-cli-usage\ndescription: x\n---\n", "document", "must open with a '---'")
	// No closing delimiter.
	wantFrontmatterError(t, "---\nname: anvil-cli-usage\ndescription: x\n", "document", "no closing '---'")
	// Empty frontmatter.
	wantFrontmatterError(t, "---\n---\ncontent\n", "name", "required field is missing")
	// Not a mapping.
	wantFrontmatterError(t, "---\n- a\n- b\n---\n", "document", "must be a YAML mapping")
	// Not decodable YAML.
	wantFrontmatterError(t, "---\nname: [unclosed\n---\n", "document", "not decodable YAML")
	// CRLF line endings are handled.
	fm, err := ParseFrontmatter([]byte("---\r\nname: anvil-cli-usage\r\ndescription: x\r\n---\r\ncontent\r\n"))
	if err != nil {
		t.Fatalf("ParseFrontmatter rejected CRLF frontmatter: %v", err)
	}
	if fm.Name != "anvil-cli-usage" {
		t.Fatalf("CRLF frontmatter name = %q", fm.Name)
	}
}

func TestParseFrontmatter_ProvenanceCommentAllowed(t *testing.T) {
	// The injected provenance header is a YAML comment; a document that
	// already carries one (e.g. after a previous install) still validates.
	doc := `---
# source: anvil 1.0.0
name: anvil-cli-usage
description: x
---
content
`
	fm, err := ParseFrontmatter([]byte(doc))
	if err != nil {
		t.Fatalf("ParseFrontmatter rejected a provenance-comment frontmatter: %v", err)
	}
	if fm.Name != "anvil-cli-usage" {
		t.Fatalf("name = %q", fm.Name)
	}
}

func TestParseFrontmatter_EmptyKeyRejected(t *testing.T) {
	// An explicit empty mapping key (YAML "? \n: value") is malformed:
	// it must be rejected, not silently skipped.
	doc := "---\n? \n: value\nname: anvil-cli-usage\ndescription: x\n---\n"
	wantFrontmatterError(t, doc, "document", "empty mapping key")
}

// ── Provenance injection ─────────────────────────────────────────────

func TestInjectProvenance_InsertsHeader(t *testing.T) {
	out, err := InjectProvenance([]byte(validSkillMD()), "anvil-standard-laravel", "5.1.0")
	if err != nil {
		t.Fatalf("InjectProvenance failed: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "# source: anvil-standard-laravel 5.1.0") {
		t.Fatalf("provenance header missing:\n%s", text)
	}
	// The header must be inside the frontmatter (after the opening
	// delimiter), and the rest of the document preserved.
	if !strings.HasPrefix(text, "---\n# source: anvil-standard-laravel 5.1.0\nname: anvil-cli-usage") {
		t.Fatalf("header not placed inside the frontmatter:\n%s", text)
	}
	if !strings.HasSuffix(text, "# Anvil CLI Usage\n\nContent here.\n") {
		t.Fatalf("document body not preserved:\n%s", text)
	}
	// The injected copy still validates as portable frontmatter.
	if _, err := ParseFrontmatter(out); err != nil {
		t.Fatalf("provenance-injected copy fails validation: %v", err)
	}
}

func TestInjectProvenance_ReplacesExistingHeader(t *testing.T) {
	doc := `---
# source: stale-standard 0.1.0
name: anvil-cli-usage
description: x
---
content
`
	out, err := InjectProvenance([]byte(doc), "anvil", "2.0.0")
	if err != nil {
		t.Fatalf("InjectProvenance failed: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "# source: anvil 2.0.0") {
		t.Fatalf("header not replaced:\n%s", text)
	}
	if strings.Contains(text, "stale-standard") {
		t.Fatalf("stale header still present:\n%s", text)
	}
	if strings.Count(text, "# source:") != 1 {
		t.Fatalf("expected exactly one provenance header:\n%s", text)
	}
}

func TestInjectProvenance_RejectsInvalidProvenance(t *testing.T) {
	if _, err := InjectProvenance([]byte(validSkillMD()), "Bad_Source", "1.0.0"); err == nil {
		t.Fatal("InjectProvenance accepted an invalid source")
	}
	if _, err := InjectProvenance([]byte(validSkillMD()), "anvil", "v1.0.0"); err == nil {
		t.Fatal("InjectProvenance accepted an invalid version")
	}
	if _, err := InjectProvenance([]byte("no frontmatter"), "anvil", "1.0.0"); err == nil {
		t.Fatal("InjectProvenance accepted a document without frontmatter")
	}
}

func TestInjectProvenance_IndentedCommentReplaced(t *testing.T) {
	// YAML allows indented comments; an existing indented provenance
	// comment is still recognized and replaced.
	doc := "---\n  # source: old 0.0.1\nname: anvil-cli-usage\ndescription: x\n---\n"
	out, err := InjectProvenance([]byte(doc), "anvil", "1.0.0")
	if err != nil {
		t.Fatalf("InjectProvenance failed: %v", err)
	}
	text := string(out)
	if strings.Contains(text, "old 0.0.1") || !strings.Contains(text, "# source: anvil 1.0.0") {
		t.Fatalf("indented provenance comment not replaced:\n%s", text)
	}
}

func TestInjectProvenance_CRLFDocument(t *testing.T) {
	doc := "---\r\nname: anvil-cli-usage\r\ndescription: x\r\n---\r\ncontent\r\n"
	out, err := InjectProvenance([]byte(doc), "anvil", "1.0.0")
	if err != nil {
		t.Fatalf("InjectProvenance failed on CRLF: %v", err)
	}
	if !strings.Contains(string(out), "# source: anvil 1.0.0") {
		t.Fatalf("header missing in CRLF document:\n%q", string(out))
	}
}

func TestInjectProvenance_ReplacePreservesLineEnding(t *testing.T) {
	// The replaced comment is the LAST line before the closing delimiter:
	// the old line's terminator must be preserved, otherwise the "---"
	// delimiter is swallowed and the installed SKILL.md is silently
	// corrupted. The result must be byte-exact and re-validate.
	doc := "---\nname: anvil-cli-usage\ndescription: x\n# source: old 0.1.0\n---\ncontent\n"
	want := "---\nname: anvil-cli-usage\ndescription: x\n# source: anvil 1.0.0\n---\ncontent\n"
	out, err := InjectProvenance([]byte(doc), "anvil", "1.0.0")
	if err != nil {
		t.Fatalf("InjectProvenance failed: %v", err)
	}
	if string(out) != want {
		t.Fatalf("byte-exact mismatch:\n got %q\nwant %q", string(out), want)
	}
	if _, err := ParseFrontmatter(out); err != nil {
		t.Fatalf("replaced frontmatter fails portable validation: %v", err)
	}
}

func TestInjectProvenance_ReplacePreservesCRLFTerminator(t *testing.T) {
	// Same replacement on a CRLF document: the "\r\n" terminator of the
	// old line is preserved, not collapsed to "\n".
	doc := "---\r\nname: anvil-cli-usage\r\ndescription: x\r\n# source: old 0.1.0\r\n---\r\ncontent\r\n"
	want := "---\r\nname: anvil-cli-usage\r\ndescription: x\r\n# source: anvil 1.0.0\r\n---\r\ncontent\r\n"
	out, err := InjectProvenance([]byte(doc), "anvil", "1.0.0")
	if err != nil {
		t.Fatalf("InjectProvenance failed on CRLF: %v", err)
	}
	if string(out) != want {
		t.Fatalf("byte-exact mismatch:\n got %q\nwant %q", string(out), want)
	}
	if _, err := ParseFrontmatter(out); err != nil {
		t.Fatalf("replaced frontmatter fails portable validation: %v", err)
	}
}

func TestInjectProvenance_ReplaceMidContentKeepsOtherLines(t *testing.T) {
	// A provenance comment in the middle of the frontmatter: everything
	// above and below it is preserved byte-for-byte.
	doc := "---\nname: anvil-cli-usage\n# source: old 0.1.0\ndescription: x\nlicense: MIT\n---\ncontent\n"
	want := "---\nname: anvil-cli-usage\n# source: anvil 1.0.0\ndescription: x\nlicense: MIT\n---\ncontent\n"
	out, err := InjectProvenance([]byte(doc), "anvil", "1.0.0")
	if err != nil {
		t.Fatalf("InjectProvenance failed: %v", err)
	}
	if string(out) != want {
		t.Fatalf("byte-exact mismatch:\n got %q\nwant %q", string(out), want)
	}
}
