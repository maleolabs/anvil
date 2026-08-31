// Package skills provides the embedded core skill set (ADR-037 D2):
// the Anvil CLI's own skills, shipped inside the binary via go:embed and
// lockstep with the CLI version they describe (a separable update cycle
// would drift).
//
// Layout. The embedded tree lives at internal/skills/core/ — one
// directory per skill, following the Agent Skills convention
// (agentskills.io): <name>/SKILL.md with portable YAML frontmatter (name,
// description, and the optional portable fields; ADR-037 D1). The CLI
// version is the version of every core skill: core content describes the
// CLI it ships with, so the installed-skill record pins Version =
// CliVersion and staleness is a version skew (TS-021-03).
//
// Authoring boundary (ST-021-02 / T-007). The full authored core content
// (overview, lifecycle, best practices — A3 boundary: CLI usage only) is
// delivered by ST-021-02 (T-007): adding or replacing a skill directory
// under internal/skills/core/ requires no code change here — enumeration
// is dynamic.
//
// This package only provides the embedded filesystem and its directory
// listing; frontmatter validation, provenance injection, and
// materialization belong to the command surface (cmd/skill_shared.go),
// which reuses the skillbundle frontmatter parser for the same portable
// validation the bundle extractor applies.
package skills

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed core
var coreFS embed.FS

// CoreDirName is the embedded core skills root directory name (relative
// to the package directory). It is the well-known location T-007 authors
// content into.
const CoreDirName = "core"

// CoreSkill is one embedded core skill: its identity and content files.
type CoreSkill struct {
	// Name is the skill name (the directory name, ^[a-z0-9][a-z0-9-]*$).
	Name string

	// Files maps skill-relative content paths (for example "SKILL.md",
	// "references/REFERENCE.md") to their bytes, exactly the shape the
	// agent-target writer consumes.
	Files map[string][]byte
}

// CoreSkillsFS returns the embedded core skills filesystem, rooted at the
// core directory (internal/skills/core/): each top-level entry is one
// skill directory.
//
// The embed is static, so the only failure (a missing embedded directory)
// is a build-time invariant; a nil-returning panic would be unreachable —
// the function returns an error anyway to keep callers total.
func CoreSkillsFS() fs.FS {
	sub, err := fs.Sub(coreFS, CoreDirName)
	if err != nil {
		panic(fmt.Sprintf("skills: embedded core directory %q missing (build invariant violated): %v", CoreDirName, err))
	}
	return sub
}

// ListCoreSkills enumerates every embedded core skill: each top-level
// directory under the core FS carrying a SKILL.md. Directories without a
// SKILL.md are reported as an error — a skill directory that cannot be
// materialized must not silently vanish from `anvil skill list`.
//
// Entries are returned in sorted name order (stable output).
func ListCoreSkills() ([]CoreSkill, error) {
	fsys := CoreSkillsFS()
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("skills: read embedded core skills: %w", err)
	}

	var out []CoreSkill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		skill, err := readCoreSkill(fsys, name)
		if err != nil {
			return nil, err
		}
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CoreSkillNames returns the names of the embedded core skills, sorted.
// It is a convenience for resolution and error messages; the error is the
// enumeration error of ListCoreSkills.
func CoreSkillNames() ([]string, error) {
	skills, err := ListCoreSkills()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return names, nil
}

// readCoreSkill reads one skill directory's content tree into the
// skill-relative shape the agent-target writer consumes: "SKILL.md",
// "references/…" — the directory name prefix is stripped. Directories
// under the skill root are walked recursively; symlinks are impossible in
// the embedded FS (go:embed never stores them).
func readCoreSkill(fsys fs.FS, name string) (CoreSkill, error) {
	skill := CoreSkill{Name: name, Files: map[string][]byte{}}
	root := name + "/"
	if err := fs.WalkDir(fsys, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path.Clean(p), root)
		if rel == "" {
			return fmt.Errorf("skills: skill %q contains a file at its root path", name)
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("skills: read %s: %w", p, err)
		}
		skill.Files[rel] = data
		return nil
	}); err != nil {
		return CoreSkill{}, fmt.Errorf("skills: read embedded skill %q: %w", name, err)
	}

	if _, ok := skill.Files["SKILL.md"]; !ok {
		return CoreSkill{}, fmt.Errorf("skills: embedded skill %q carries no SKILL.md — every core skill directory must contain SKILL.md (agentskills.io)", name)
	}
	return skill, nil
}

// Get returns the embedded core skill by name, or ok=false when the core
// set does not contain it.
func Get(name string) (CoreSkill, bool, error) {
	fsys := CoreSkillsFS()
	if _, err := fs.Stat(fsys, name); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return CoreSkill{}, false, nil
		}
		return CoreSkill{}, false, fmt.Errorf("skills: stat embedded skill %q: %w", name, err)
	}
	skill, err := readCoreSkill(fsys, name)
	if err != nil {
		return CoreSkill{}, false, err
	}
	return skill, true, nil
}
