# Skill Bundle Format (Draft)

## The Anvil Skill Bundle Specification — Format, Manifest, Frontmatter, and Extraction Security

| Metadata | |
|---|---|
| **Document ID** | skill-bundle-format |
| **Status** | Draft |
| **Date** | 2026-08-12 |
| **Product** | Anvil |
| **Dependencies** | [ADR-037](../adr/ADR-037-ai-agent-skills-distribution.md) (D1, D2, D4, D10) · [ADR-024](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) (§3.1 contract major) · [ADR-022](../adr/ADR-022-standard-trust-and-supply-chain-security.md) (explicit adoption) · [agentskills.io](https://agentskills.io) (Agent Skills spec) · [EPIC-021](../epics/EPIC-021-ai-agent-skills.md) |
| **Consumers** | TS-021-01 (format, validation, extraction) · ST-021-01 (`anvil skill install` gate) · ST-021-02 (core skill materialization) · ST-021-03 (standard skill release packaging) · skill publishers |

**Contract, not engine.** The Anvil Core implements and enforces this format; this document is the format the Core validates, not a description of the Core. The Go implementation in `internal/skillbundle` is the machine-checkable authority for the rules below (the same role the registry metadata parse plays for `registry-metadata`); where this document and the implementation disagree, the implementation's strict parse governs until this document is corrected.

---

## 1. Purpose

This document defines the Anvil skill bundle format: the distribution unit of one AI agent skill ([ADR-037 D2](../adr/ADR-037-ai-agent-skills-distribution.md)). A skill bundle is a single gzip-compressed tar archive whose name is pinned to `anvil-skill-<name>-<version>.tar.gz`, carrying a machine-readable manifest and the skill content tree — a folder named after the skill containing `SKILL.md` in the Agent Skills format ([agentskills.io](https://agentskills.io)).

It is written for an implementer who has never seen the engine source. Everything in this document is implementable from the contract alone:

- **One bundle, one skill.** A bundle carries exactly one skill: identity (name, version), source (the standard-id the skill ships with), format contract version, description, and the exact content inventory.
- **Portable by construction.** The SKILL.md frontmatter is restricted to the portable Agent Skills fields; agent-specific frontmatter fields are rejected so one artifact works across agents ([ADR-037 D1](../adr/ADR-037-ai-agent-skills-distribution.md)).
- **Trust from day one.** Skill assets travel in the standard's release channel, declared in registry metadata and covered by attested named-asset digests; the install gate is the full adoption pipeline ([ADR-037 D4](../adr/ADR-037-ai-agent-skills-distribution.md); [ADR-022 §3](../adr/ADR-022-standard-trust-and-supply-chain-security.md)). This document defines only the bundle format — the artifact the pipeline fetches, verifies, and extracts.
- **Extraction is a security boundary.** Untrusted standard content is materialized only through the rooted extractor (§6): no path traversal, no symlink escape, mode 0644, bounded sizes.

---

## 2. Bundle Layout

A skill bundle is a **single-member gzip-compressed tar archive** with a pinned entry layout:

```text
anvil-skill-<name>-<version>.tar.gz
├── manifest.json                      (first entry, exactly once)
└── <name>/                            (the skill content root)
    ├── SKILL.md                       (required; agentskills.io)
    └── …                              (additional content files, all under <name>/)
```

**Entry rules:**

- `manifest.json` is the first entry, exactly once.
- Every other entry lives under the content root `<name>/` — the root directory entry itself (`<name>/`) is allowed; a trailing `/` on directory entries is conventional and accepted.
- Only regular-file and directory entries are allowed. Symlink, hardlink, char, block, and fifo entries are rejected (§6.1).
- PAX and GNU extended headers are rejected: the layout is pinned and exact, not extensible.
- The archive is exactly one gzip member; trailing input of any length (a second gzip member, or garbage) is rejected, and decompressed trailing data after the tar end-of-archive markers is rejected within a bounded drain budget.

**Asset name.** The asset name is `anvil-skill-<name>-<version>.tar.gz` ([ADR-037 D2](../adr/ADR-037-ai-agent-skills-distribution.md)). Because `<version>` is pinned to semver (digits and dots only), the trailing semver is an unambiguous split point even when the name contains hyphens. The install flow parses the asset name and must verify it matches the manifest's `name` and `version`.

---

## 3. Identifiers and Patterns

| Rule | Pattern | Notes |
|---|---|---|
| **Skill name** | `^[a-z0-9][a-z0-9-]*$` | Lowercase alphanumeric with hyphens; the same convention as the registry corpus id (registry-metadata §4.1). Max 64 bytes. Also the content root directory name. |
| **Version** | `^(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)$` | Semver without leading zeros — the exact registry version pattern (registry-metadata §4.3). Max 64 bytes. |
| **Source** | `^[a-z0-9][a-z0-9-]*$` | The standard-id the skill ships with: `anvil` for core skills, the standard id otherwise. Max 64 bytes. Becomes the provenance header on the installed SKILL.md. |
| **Contract version** | `^(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)$` | Semver without leading zeros, like `version`. The skill-bundle-format contract the bundle targets. Major must be the supported contract major (currently `1`). |

---

## 4. Manifest Document (`manifest.json`)

The manifest is the bundle's identity card and content inventory. It is a JSON object with **exactly** these six fields (`additionalProperties: false` — unknown fields are rejected):

| Field | Type | Required | Rules |
|---|---|---|---|
| `name` | string | yes | §3 name pattern; also the content root directory name. |
| `version` | string | yes | §3 version pattern. |
| `source` | string | yes | §3 source pattern. |
| `contractVersion` | string | yes | §3 contract-version pattern; supported major `1` (ADR-024 §3.1: the contract major is the unit of compatibility). |
| `description` | string | yes | Non-empty, max 512 bytes. |
| `files` | array of strings | yes | The exact content inventory: at least one entry, all unique, each a safe relative path (§4.1); **exactly one** entry must be `<name>/SKILL.md`. |

### 4.1 Content inventory rules

Every `files[]` entry must:

- be a safe relative path: no leading `/`, no drive-letter prefix, no backslash, no `.`/`..`/empty components, no control characters (§6.1 character rules);
- live under the content root `<name>/`;
- be at most 256 bytes long and at most 16 components deep.

The inventory is exact both ways at extraction: the archive must carry **exactly** the declared files (an undeclared entry is rejected, a declared-but-missing file is rejected).

---

## 5. SKILL.md Frontmatter

The skill's `SKILL.md` follows the Agent Skills spec ([agentskills.io](https://agentskills.io)): a YAML frontmatter block delimited by `---` lines, restricted to the **portable** fields:

| Field | Required | Type | Rules |
|---|---|---|---|
| `name` | yes | string | §3 name pattern; must equal the manifest `name`. |
| `description` | yes | string | Non-empty. |
| `license` | no | string | Optional SPDX expression. |
| `compatibility` | no | mapping | Optional; shape owned by the Agent Skills spec. |
| `metadata` | no | mapping | Optional; shape owned by the Agent Skills spec. |
| `allowed-tools` | no | sequence of strings | Optional. |

**Portability rule ([ADR-037 D1](../adr/ADR-037-ai-agent-skills-distribution.md)).** Any other frontmatter field — agent-specific fields such as Claude Code's `context`, or a literal `source` field — is rejected with an actionable error, so one artifact stays portable across agents. The frontmatter must decode as a YAML mapping; a missing closing delimiter, an empty block, or a non-mapping block is rejected.

### 5.1 Provenance header — install-time only

The provenance header `source: <standard-id> <version>` ([ADR-037 D10](../adr/ADR-037-ai-agent-skills-distribution.md)) is **enforced at install, not at author time**:

- it is **not** a portable field, so it is never authored in the bundle's SKILL.md (a literal `source` field is rejected by §5);
- during extraction, the header is **injected** into the installed copy as a YAML comment — `# source: <standard-id> <version>` — placed inside the frontmatter block, where `<standard-id>` is the manifest `source` and `<version>` the manifest `version` (ADR-037 D10). A comment is not a field, so the installed copy remains portable and re-injection is idempotent (an existing `# source:` comment is replaced).
- The injection refuses malformed provenance: an invalid source or version is never injected.

The installed SKILL.md therefore differs from the archive copy exactly by this one comment line; everything else is preserved byte-for-byte.

---

## 6. Extraction Security

Extraction is the security boundary that turns a verified-but-untrusted artifact into installed content ([ADR-037 D4](../adr/ADR-037-ai-agent-skills-distribution.md)). The extractor is **rooted**: every write is contained under the resolved extraction root, and every rule below is enforced during extraction, before and while data is written.

### 6.1 Entry path rules

Every archive entry path (and every manifest `files[]` entry) must satisfy:

- **relative** — no leading `/`, no drive-letter prefix (`C:...`), no backslash (a Windows-style separator smuggled into a POSIX archive);
- **no traversal** — no `..`, `.`, or empty path components;
- **no control characters** in any component;
- length ≤ 256 bytes, depth ≤ 16 components.

Containment is then verified against the **resolved** extraction root (`EvalSymlinks`), so even a symlinked parent of the staging directory cannot redirect a write outside the root.

### 6.2 Symlink and link escape

- Symlink, hardlink, char, block, and fifo entries are **rejected outright** — the extractor never creates a link, so no entry can point a later write outside the root.
- The extraction root is resolved before any write, and files are created with `O_CREATE|O_EXCL`: a pre-existing file at a target path, a duplicate archive entry, or a dir/file conflict is a **hard error, never a silent overwrite**.

### 6.3 File modes

- Content files are written **mode 0644** — the executable bit is stripped whatever the archive claims (ADR-037 D4).
- Directories are written mode 0755.

### 6.4 Size caps (enforced during extraction)

| Cap | Value | Enforcement |
|---|---|---|
| Per-asset | 10 MiB | Checked from the entry header and re-checked while copying (a lying size cannot exceed the cap) |
| Total content | 64 MiB | Checked incrementally as each file is written |
| File count | 512 | Checked incrementally |
| **Total entries** | **1024** | **Checked incrementally for every archive entry — files AND directory entries — so a hostile archive padded with directory entries is bounded like any other** |
| Path length / depth | 256 bytes / 16 components | Checked before any write |

The single gzip member is drained within a bounded budget, so decompression work is bounded even for a hostile archive.

### 6.5 Validation pipeline

The extractor validates in a fixed order, each stage with actionable errors:

1. **Archive structure** — gzip/tar shape, manifest first, no extended headers, safe entry paths, no links/devices, entries within the content root and exactly matching the manifest inventory.
2. **Manifest** — the strict §4 parse.
3. **Caps** — §6.4, during extraction.
4. **Frontmatter** — the §5 parse on the extracted `SKILL.md`, plus the name match against the manifest.
5. **Provenance injection** — §5.1.

On any failure the extractor removes everything it created (best effort) — a failed extraction never leaves a partial skill tree behind — and the destination is left as it was found. The caller supplies an empty or fresh staging directory; materialization into the agent scope is the caller's step (ST-021-02/ST-021-03).

---

## 7. Failure Classes

Rejected bundles are classified so failures are attributable:

| Kind | Meaning |
|---|---|
| `structure` | Not a valid bundle layout, unsafe entry path, link/device entry, extended header, entry outside the content root or not declared in the inventory, dir/file conflict |
| `integrity` | Corrupt or truncated stream, trailing input beyond the single gzip member |
| `manifest` | `manifest.json` missing, unparseable, or invalid (§4) |
| `frontmatter` | `SKILL.md` frontmatter invalid (§5) or not matching the manifest name |
| `limits` | A §6.4 cap exceeded |

---

## 8. Terminology

| Term | Definition | Source |
|---|---|---|
| **Skill bundle** | The distribution unit of one skill: `anvil-skill-<name>-<version>.tar.gz` carrying `manifest.json` and the skill content tree | ADR-037 D2; this document |
| **Content root** | The `<name>/` directory inside the bundle that contains `SKILL.md` | agentskills.io; this document |
| **Provenance header** | The `# source: <standard-id> <version>` comment injected into the installed SKILL.md at extraction | ADR-037 D10; §5.1 |
| **Portable frontmatter fields** | `name`, `description`, `license`, `compatibility`, `metadata`, `allowed-tools` | agentskills.io; ADR-037 D1 |

---

## 9. Traceability

- ADR-037 D1 (portable frontmatter) → §5; ADR-037 D2 (asset name + bundle) → §2; ADR-037 D4 (extraction security) → §6; ADR-037 D10 (provenance header) → §5.1.
- ADR-024 §3.1 (contract major = unit of compatibility) → §4 `contractVersion`.
- ADR-022 §3 (explicit adoption, no side channels) → §1.
- TS-021-01 (format, validation, extraction) implements this document; ST-021-02/ST-021-03 consume it.

*End of skill-bundle-format*
