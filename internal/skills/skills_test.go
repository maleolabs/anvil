package skills

import (
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/skillbundle"
)

// expectedCoreSkills is the authored core skill set (ST-021-02 / T-007):
// one directory per skill under internal/skills/core/. Adding a skill here
// requires content authorship AND this list update — the set is pinned so a
// skill cannot silently vanish from (or be added to) the embedded core set.
var expectedCoreSkills = map[string]bool{
	"anvil-overview":       true, // what Anvil is, command groups, quick start
	"anvil-lifecycle":      true, // delivery stages + standard lifecycle states
	"anvil-best-practices": true, // adoption, conventions, automation, skills workflow
}

// TestListCoreSkills enumerates the embedded core set: exactly the authored
// set, each with its SKILL.md, in the writer-consumable skill-relative
// shape.
func TestListCoreSkills(t *testing.T) {
	all, err := ListCoreSkills()
	if err != nil {
		t.Fatalf("ListCoreSkills: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("ListCoreSkills returned no core skills — the embedded set is empty")
	}

	seen := make(map[string]bool, len(all))
	for _, s := range all {
		if !expectedCoreSkills[s.Name] {
			t.Errorf("unexpected core skill %q — update expectedCoreSkills when the authored set changes", s.Name)
			continue
		}
		if seen[s.Name] {
			t.Errorf("duplicate core skill %q in the enumeration", s.Name)
		}
		seen[s.Name] = true

		md, ok := s.Files["SKILL.md"]
		if !ok {
			t.Fatalf("core skill %s has no SKILL.md in the content map", s.Name)
		}
		content := string(md)
		if !strings.Contains(content, "name: "+s.Name) {
			t.Errorf("SKILL.md frontmatter lacks the name field:\n%s", content)
		}
		if !strings.Contains(content, "description:") {
			t.Errorf("SKILL.md frontmatter lacks a description:\n%s", content)
		}
	}
	for name := range expectedCoreSkills {
		if !seen[name] {
			t.Errorf("the embedded core set is missing the authored skill %q", name)
		}
	}
}

// TestCoreSkillFrontmatterValid pins the frontmatter validity of every
// authored core skill against the SAME portable validation the install path
// applies (skillbundle.ParseFrontmatter + name match — ADR-037 D1). A
// literal "source" frontmatter field is rejected by the parser: provenance
// is injected at install time as a YAML comment, never authored
// (skill-bundle-format.md §5.1).
func TestCoreSkillFrontmatterValid(t *testing.T) {
	all, err := ListCoreSkills()
	if err != nil {
		t.Fatalf("ListCoreSkills: %v", err)
	}
	for _, s := range all {
		md, ok := s.Files["SKILL.md"]
		if !ok {
			t.Fatalf("core skill %s has no SKILL.md in the content map", s.Name)
		}
		fm, err := skillbundle.ParseFrontmatter(md)
		if err != nil {
			t.Errorf("core skill %s: portable frontmatter validation rejects SKILL.md: %v", s.Name, err)
			continue
		}
		if fm.Name != s.Name {
			t.Errorf("core skill %s declares frontmatter name %q — identity must match the directory", s.Name, fm.Name)
		}
		if strings.TrimSpace(fm.Description) == "" {
			t.Errorf("core skill %s: description must be non-empty", s.Name)
		}
	}
}

// TestGetCoreSkill resolves a core skill by name and reports a clean miss
// for unknown names.
func TestGetCoreSkill(t *testing.T) {
	skill, ok, err := Get("anvil-overview")
	if err != nil {
		t.Fatalf("Get(anvil-overview): %v", err)
	}
	if !ok {
		t.Fatal("Get(anvil-overview): not found")
	}
	if skill.Name != "anvil-overview" {
		t.Errorf("Get name = %q", skill.Name)
	}
	if _, ok := skill.Files["SKILL.md"]; !ok {
		t.Error("Get: SKILL.md missing from content map")
	}

	if _, ok, err := Get("no-such-skill"); err != nil || ok {
		t.Fatalf("Get(no-such-skill) = (_, %v, %v), want (_, false, nil)", ok, err)
	}
}

// TestCoreSkillNamesIsSorted pins the stable, sorted enumeration used by
// resolution and error messages.
func TestCoreSkillNamesIsSorted(t *testing.T) {
	names, err := CoreSkillNames()
	if err != nil {
		t.Fatalf("CoreSkillNames: %v", err)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("CoreSkillNames not sorted: %v", names)
		}
	}
}
