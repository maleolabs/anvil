# Registry Metadata (Draft)

## The Delivery Lifecycle Specification — Registry Metadata Format

| Metadata | |
|---|---|
| **Document ID** | registry-metadata |
| **Status** | Draft |
| **Date** | 2026-08-04 |
| **Product** | Anvil |
| **Dependencies** | [PRD-002 §5.7, §5.8](../prd/PRD-002-anvil-v2.md) · [ANVIL_V2_TRANSITION_PLAN §3 A4, §5.7, §5.10, §6.5](../planning/ANVIL_V2_TRANSITION_PLAN.md) · [ANVIL_MANIFESTO §3.2, §3.4](../manifesto/ANVIL_MANIFESTO.md) · [ADR-021](../adr/ADR-021-delivery-lifecycle-standard-model.md) · [ADR-022](../adr/ADR-022-standard-trust-and-supply-chain-security.md) · [ADR-023](../adr/ADR-023-delivery-lifecycle-standard-registry.md) · [ADR-024](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md) · [ADR-027](../adr/ADR-027-delivery-lifecycle-standard-governance.md) · [ADR-029](../adr/ADR-029-specification-publication-format.md) · [ADR-030](../adr/ADR-030-registry-distribution-channel.md) · [ADR-035](../adr/ADR-035-governance-and-identity-reframing-amendments.md) · [artifact-manifest](artifact-manifest.md) · [command-contract](command-contract.md) |
| **Consumers** | EPIC-014 (registry metadata format, distribution, adoption validation) · TS-014-01-02 (parsing and validation) · TS-014-01-03 (lifecycle state behavior) · delivery lifecycle standard publishers · registry index authors |

**Docs/schema authority rule (ADR-029 §3).** The delivery lifecycle specification is published in dual form — human-readable documentation plus a machine-readable JSON Schema. The JSON Schema is the machine-readable authority: **where this document and the schema disagree, the schema governs.** This document describes the format; it does not describe the engine.

---

## 1. Purpose

This document is the registry metadata format part of the delivery lifecycle specification: the definition of the machine-readable document that describes one release of one delivery lifecycle standard in the standard registry ([PRD-002 §5.7](../prd/PRD-002-anvil-v2.md); [ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md)).

It is written for an implementer who has never seen the engine source. Everything in this document is implementable from the contract alone:

- **One document, one release.** A registry metadata document describes one release of one standard — identity, version, declared contract version, capability declaration, distribution location, lifecycle state, and trust fields.
- **Runtime-agnostic.** The format is pure data: no Go-specific fields, no engine paths, no engine internals. Independent runtime implementations remain structurally possible (ADR-023 §3; Transition Plan §5.10).
- **Trust from day one.** Trust fields are part of the schema from the outset — introduced with the registry, not retrofitted (ADR-022 §3, §6.5).
- **Metadata, not content.** The registry is distribution metadata, not content hosting; content is resolved from the standard's own release channel (ADR-030 §3).

**Contract, not engine.** The registry client implements and enforces this format; this document is the format the client validates, not a description of the client ([ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)). The machine-checkable schema (TS-014-01-01) is the authoritative form; this document describes the semantics the schema encodes.

**Relationship to the registry.** ADR-023 defines the registry concept — metadata format, discovery, installation/update semantics, offline/bundled install path. This document defines only the metadata format. The wire protocol between client and index, the discovery mechanics, and the installation semantics are EPIC-014 implementation scope (TS-014-01-01 §2).

---

## 2. Authority, Publication, and Governance

### 2.1 Position in the three-layer model

The registry metadata format is part of the **delivery lifecycle specification** — the authority every other layer conforms to ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md); [ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md)):

```text
Delivery Lifecycle Specification      ← what a legal lifecycle IS (the authority)
        │  implemented by                 this document is part of this layer
        ▼
Anvil Runtime (Core)                    ← the engine: enforces the specification,
        │  executes                        runs the registry client
        ▼
delivery lifecycle standards         ← framework lifecycle content for one
        (anvil-standard-*)                 framework
```

The specification defines the registry metadata format; the runtime implements this contract through its registry client; standards publish releases described by this format ([Transition Plan §5.1](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

### 2.2 Publication format

- The registry metadata format is published in **dual form**: this human-readable document plus a machine-readable JSON Schema. The schema is the machine-readable authority; where the two disagree, the schema governs ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md)).
- The specification corpus is authored **engine-path-independent**: it references no engine paths and no engine internals, so a future re-home of the specification is a move, not a rewrite ([ADR-029 §3](../adr/ADR-029-specification-publication-format.md); [Transition Plan §5.2, §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md)).
- The corpus is authored in the Core repository; there is no separate specification repository ([Transition Plan §5.2](../planning/ANVIL_V2_TRANSITION_PLAN.md)).

### 2.3 Versioning

- The specification carries its own independent semver version line, decoupled from runtime releases; the contract major version is the unit of compatibility ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- The runtime implements at most **two concurrently supported contract major versions**; a superseded contract major remains supported for one full contract generation — the deprecation window ([ADR-024 §3.4](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).
- Specification artifacts are published via a separate `spec/` tag line; engine and specification artifacts never share a tag ([ADR-024 §3.5](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

### 2.4 Governed artifact

- The delivery lifecycle specification is a **governed architecture artifact of the Core repository** ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md)).
- A breaking change to the registry metadata format — any change to the document structure, trust semantics, or lifecycle semantics that breaks compatibility — is a **governed event**: it requires an ADR and ships with a Core major version ([ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md); [ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)).

---

## 3. Terminology

This section uses the vocabulary of the delivery lifecycle specification; the full vocabulary is owned by Core and standards must not redefine its semantics ([PRD-002 §5.5](../prd/PRD-002-anvil-v2.md)).

| Term | Definition | Source |
|---|---|---|
| **Registry metadata document** | The machine-readable document described by this contract: one release of one delivery lifecycle standard | ADR-023 §3; this document |
| **Delivery lifecycle standard** | The distributable unit of framework lifecycle knowledge for one framework; a release of a standard is described by one registry metadata document | ADR-021 §3.1; Transition Plan §5.1 |
| **Declared contract version** | The version of the delivery lifecycle specification a standard's release targets | ADR-024 §3.1; PRD-002 §5.8 |
| **Capability declaration** | The declared compatibility surface of a release: contract-version and framework-version support scope | ADR-021 §3.2; PRD-002 §5.8 |
| **Distribution location** | Where the release content is resolved from: the standard's own release channel, per the distribution channel decision | ADR-030 §3 |
| **Lifecycle state** | The governed availability state of a standard release: Published, Deprecated, or Retired | ADR-021 §3.3; ADR-023 §3; ADR-027 §3 |
| **Trust fields** | The integrity and attestation material a release carries: content digest(s), publisher attestation signature, publisher verification public key | ADR-022 §3 |

---

## 4. Document Structure

A registry metadata document is a single JSON object. The top-level fields are:

| Field | Required | Type | Meaning |
|---|---|---|---|
| `id` | yes | string | Standard identity (stable across releases) |
| `version` | yes | string (semver) | Release version |
| `contractVersion` | yes | string (semver) | Declared contract version of the delivery lifecycle specification |
| `capability` | yes | object | Capability declaration: framework-version support scope |
| `distribution` | yes | object | Distribution location of the release content |
| `lifecycle` | yes | object | Lifecycle state of the release |
| `trust` | yes | object | Trust fields: content digest(s) and publisher attestation |

Optional annotation fields follow the corpus convention: `$schema` (the `$id` of the schema this document targets), `title`, and `description` (human-readable annotations). They carry no validation semantics.

The document is minimal by design: every field in §4.1–§4.7 is required because every one of them is needed at adoption — identity and version (idempotent, pinned installation), contract version and capability (compatibility validation), distribution location (content resolution), lifecycle state (availability semantics), and trust fields (integrity and attestation).

### 4.1 Identity: `id`

The stable identifier of the delivery lifecycle standard this release belongs to (e.g. `anvil-standard-laravel`; [ADR-021 §3.1](../adr/ADR-021-delivery-lifecycle-standard-model.md): standards are named `anvil-standard-*`). The `id` is stable across releases of the same standard.

- Constraint: lowercase alphanumeric with hyphens — `^[a-z0-9][a-z0-9-]*$` (corpus id convention), at most 64 characters (`maxLength: 64`). The pattern rejects traversal-style separators (dots, slashes, path separators); the length bound keeps ids bounded for index use.
- The `id` is the identity half of the installation idempotency key **standard identity plus version** ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md)): installation of the same `id` at the same `version` is idempotent.
- The `id` is distinct from any content hash in `trust.contentDigests`: identity comes from the standard's identity declaration, not from content or location ([ADR-030 §3](../adr/ADR-030-registry-distribution-channel.md); [Manifesto §3.4](../manifesto/ANVIL_MANIFESTO.md)).

### 4.2 Version: `version`

The release version of the standard, semver (major.minor.patch) without leading zeros.

- Constraint: `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$` (`maxLength: 64`). A version with leading zeros is ambiguous for pinning and comparison at adoption and is rejected.
- Standards are independently versioned — standard releases and runtime releases are decoupled; a runtime major version may coexist with multiple standard versions during the deprecation window ([ADR-021 §3.4](../adr/ADR-021-delivery-lifecycle-standard-model.md); [ADR-025 §3](../adr/ADR-025-repository-split-core-vs-standards.md)).
- The `version` is the second half of the installation idempotency key ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md)).
- Adoptions pin standard versions; resolution is explicit and recorded ([ADR-022 §3](../adr/ADR-022-standard-trust-and-supply-chain-security.md)).

### 4.3 Declared contract version: `contractVersion`

The version of the delivery lifecycle specification this release targets ([ADR-024 §3.1](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md)): semver without leading zeros, decoupled from runtime releases; the major version is the compatibility unit.

- Constraint: `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$` (`maxLength: 64`).
- A standard is valid only against a published contract version; compatibility is negotiated at adoption — registry validation plus runtime verification ([ADR-021 §3.4](../adr/ADR-021-delivery-lifecycle-standard-model.md); [ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md)).
- A release that declares an unsupported contract version is rejected at adoption ([PRD-002 §5.8](../prd/PRD-002-anvil-v2.md); [ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md)).

### 4.4 Capability declaration: `capability`

The declared compatibility surface of the release ([ADR-021 §3.2](../adr/ADR-021-delivery-lifecycle-standard-model.md)). The capability declaration carries contract-version and framework-version support scope, enabling real compatibility validation at adoption ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md); [PRD-002 §5.8](../prd/PRD-002-anvil-v2.md): every standard declares the contract version it targets and the framework versions it supports; a standard that does not declare compatibility is rejected).

| Field | Required | Type | Meaning |
|---|---|---|---|
| `frameworkVersion` | yes | array of strings (semver), at least one, unique | Framework-version support scope of this release |

The contract version is declared at the document top level (`contractVersion`); this object declares the framework-version support scope. The runtime-level operational declaration (activation phases, verification checks, command exchange) is the standard command contract's scope ([command-contract](command-contract.md); [command-contract.schema.json](command-contract.schema.json)), not registry metadata.

- Constraint: `frameworkVersion` must contain at least one item, all unique, each `^[0-9]+\.[0-9]+\.[0-9]+$` (follows the command-contract `frameworkVersion` convention).
- A release that omits or malforms the scope is rejected at adoption ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md)).

### 4.5 Distribution location: `distribution`

Where the release content is resolved from ([ADR-030 §3](../adr/ADR-030-registry-distribution-channel.md)). The registry is distribution metadata, not content hosting: metadata lives in repositories and resolves content from the standard's own release channel; content integrity is enforced at install by ADR-022 trust validation, not by the distribution channel itself.

| Field | Required | Type | Meaning |
|---|---|---|---|
| `type` | yes | string (enum) | Distribution channel pattern; the only supported value is `github-releases` |
| `location` | yes | string (URI, https-only) | URL of the release content on the declared channel |

- `type` documents the channel pattern. ADR-030 fixes the current pattern: standard content follows the existing GitHub-releases release workflow, so standard releases reuse release mechanics already in place ([ADR-030 §3, §5](../adr/ADR-030-registry-distribution-channel.md)). A new channel pattern is a schema evolution (governed event, §2.4).
- `location` is the URL of the release content on the declared channel. The scheme is **pinned to `https`** — the schema pattern (`^https://`) rejects `http`, `file`, and any other scheme, so release content is always resolved over TLS. The `format: uri` annotation is advisory; the scheme pin is the enforceable constraint.
- `location` is a distribution address, not an identity: identity comes from the standard's `id` and the trust fields, not from the location ([Manifesto §3.4](../manifesto/ANVIL_MANIFESTO.md); [ADR-030 §3](../adr/ADR-030-registry-distribution-channel.md)).

### 4.6 Lifecycle state: `lifecycle`

The governed availability state of the release ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md); [ADR-027 §3](../adr/ADR-027-delivery-lifecycle-standard-governance.md); [PRD-002 §5.7](../prd/PRD-002-anvil-v2.md)).

| Field | Required | Type | Meaning |
|---|---|---|---|
| `state` | yes | string (enum) | `published`, `deprecated`, or `retired` |
| `removalDate` | no | string (ISO 8601 date-time) | Announced removal date of a deprecated standard |

- `state` machine values are lowercase: `published` (discoverable, installable, validated against the declared contract version), `deprecated` (installable with warning; no updates; removal date announced), `retired` (removed from the registry with a documented migration path) ([ADR-027 §3](../adr/ADR-027-delivery-lifecycle-standard-governance.md)). The capitalized prose forms (Published/Deprecated/Retired) denote the same states.
- `removalDate` is the announced removal date of a deprecated standard ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md): Deprecated standards carry a removal date; [ADR-027 §3](../adr/ADR-027-delivery-lifecycle-standard-governance.md)). It is **only allowed when `state` is `deprecated`** — the schema enforces this with an if/then constraint (a removal date on a `published` or `retired` entry is invalid) — and it stays **optional** for deprecated entries per PM decision D-03: the field SHOULD be present once the removal date is announced. The `format: date-time` annotation is advisory: draft-07 format keywords are annotations unless the validator applies a format checker; strict date parsing is the registry client's responsibility (TS-014-01-02).
- The schema reflects the governed states; client-side lifecycle behavior (deprecation warnings at discovery and install, no updates for deprecated standards, retired standards not resolvable for fresh adoption) is implemented by the registry client ([TS-014-01-03](../work-items/technical-stories/TS-014-01-03-standard-lifecycle-states-in-registry-metadata.md)).

### 4.7 Trust fields: `trust`

The trust fields ([ADR-022 §3](../adr/ADR-022-standard-trust-and-supply-chain-security.md)): integrity verification material, publisher attestation, and version pinning are part of the registry metadata schema from the start — introduced with the registry, not retrofitted (ADR-022 §3, §6.5). The trust model applies equally to first-party and community standards; there is no privileged path ([ADR-022 §3](../adr/ADR-022-standard-trust-and-supply-chain-security.md)).

The cryptographic baseline (ADR-022 §7 defers the mechanism to implementation design; fixed here per PM decision D-01):

- **Integrity** — SHA-256 content digest(s) of the release content.
- **Publisher attestation** — an Ed25519 signature over a canonical payload that binds the release claims together with the content digest(s).
- **Publisher verification public key** — the publisher's Ed25519 public key, carried in the document.
- **Key rotation is out of scope** for this version of the schema: a single verification key, no key identifiers.

| Field | Required | Type | Meaning |
|---|---|---|---|
| `contentDigests` | yes | array of digest objects, at least one, unique | Content digest(s) of the release content |
| `attestation` | yes | object | Publisher attestation: signature, algorithm, verification public key |

**Content digests (`contentDigests`).** Each digest entry is computed over the release content resolved from `distribution.location`, or over a named release asset of the same release (TS-014-04-04 — e.g. the adapter binaries `anvil-adapter-<framework>-<os>-<arch>`); the optional `name` field binds the entry to the asset. Entries carry:

| Field | Required | Meaning |
|---|---|---|
| `algorithm` | yes | `sha-256` (fixed by the trust baseline; no other algorithm is supported) |
| `encoding` | yes | `base16`, `base32`, or `base64` (base16/hex is the conventional default) |
| `digest` | yes | The SHA-256 digest in the declared encoding; must be non-empty |
| `name` | no | Binds the entry to a named release asset of the same release (e.g. `anvil-adapter-laravel-linux-amd64`); absent for content digests of the release content |

The `name`, when present, is a safe asset identifier — `^[a-z0-9][a-z0-9-]*$`, at most 128 characters — so it can never escape the release channel as a path component, and names must be unique (two entries cannot bind the same asset). Releases published before binary attestation carry no named entries and remain valid (TS-014-04-04 backward compatibility).

The digest value is constrained per its declared encoding — the schema enforces these with if/then constraints:

| Encoding | Digest value constraint |
|---|---|
| `base16` | `^[0-9a-f]{64}$` — exactly 64 **lowercase** hex characters |
| `base32` | `^[A-Z2-7]{52,56}=*$` — RFC-4648 base32 encoding of a 32-byte digest (52 data characters plus up to 4 padding `=` characters) |
| `base64` | `^[A-Za-z0-9+/]{43}=$` — RFC-4648 standard alphabet with padding (43 data characters plus one `=` padding) |

**All-match semantics.** At adoption, **every** entry **without a `name`** must match the recomputed content hash of the release content — all-match, not any-match: if any entry does not match, verification fails (ADR-022 §3; a claim is not evidence — [Manifesto §3.2, §3.4](../manifesto/ANVIL_MANIFESTO.md); [artifact-manifest §5.1](artifact-manifest.md)). Entries may be different encodings of the same digest value — the array exists for encoding interoperability, not for alternative digests. At least one digest is required — a release without integrity material cannot be verified at install and is rejected ([ADR-022 §3](../adr/ADR-022-standard-trust-and-supply-chain-security.md); [ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md): offline/bundled installs carry the same integrity verification). A release whose entries are ALL named assets declares no release-content digest and is rejected.

**Named asset digests (TS-014-04-04).** Named entries extend the publisher attestation to the release's binary assets: the canonical attestation payload concatenates **every** decoded digest in array order, prefixed by the entry's signed name, so the signature binds the archive AND each named binary. At install, each downloaded binary is verified against its named entry (all-match within the asset: the digest must match the downloaded bytes) — closing the same-channel, unsigned-`SHA256SUMS.txt` trust gap of [TS-016-04-01 §6 accepted risk 1](../work-items/technical-stories/TS-016-04-01-installer-and-discovery-switch-over.md). Because the name is signed, an attacker cannot strip it (to force the checksum fallback) or rename it across assets. An adoption encountering a release without the material degrades to the same-channel checksum with an explicit notice — never a silent trust downgrade.

**Publisher attestation (`attestation`).** The release carries attestation from its publisher, sufficient to establish origin; publishing with sufficient attestation is a standard responsibility ([ADR-022 §3, §5.6](../adr/ADR-022-standard-trust-and-supply-chain-security.md)). The attestation carries:

| Field | Required | Meaning |
|---|---|---|
| `algorithm` | yes | `ed25519` (fixed by the trust baseline; no other algorithm is supported) |
| `signature` | yes | The Ed25519 signature over the canonical attestation payload, base64-encoded (RFC-4648 standard with padding); must be non-empty |
| `publicKey` | yes | The publisher's Ed25519 verification public key, base64-encoded (RFC-4648 standard with padding); must be non-empty |

**Attestation payload.** The signature binds the release claims together with the content digest(s) — not over content alone — so it cannot be detached and replayed against a different release of the same content. The signed payload is the canonical, unambiguous byte-level composition (security review F-2):

```text
utf8(id) || 0x00 || utf8(version) || 0x00 || concat(entry bytes in contentDigests array order)
```

where `0x00` is a single NUL byte, `utf8(x)` is the UTF-8 encoding of field `x`, and each entry contributes its decoded digest bytes (base16 = lowercase hex, base32 = RFC-4648, base64 = RFC-4648 standard with padding), prefixed by `utf8(name) || 0x00` when the entry carries a `name`. **The asset binding is signed material**: stripping a `name` (forcing an adoption into the same-channel checksum fallback) or renaming an entry across assets changes the payload and invalidates the attestation. Releases published before binary attestation carry no named entries and compose byte-identically to the pre-F-2 payload — their signatures keep verifying. **Consumers verify the signature over exactly these bytes — byte-for-byte; any deviation in the composition invalidates the attestation.** For the common single-digest release, the payload is `utf8(id) || 0x00 || utf8(version) || 0x00 || <digest bytes>`. Exact signing/verification mechanics are EPIC-014 implementation scope; the format fields and the payload composition are fixed here.

**Version pinning.** Pinning is expressed by `id` + `version` (§4.1, §4.2): adoptions pin standard versions; resolution is explicit and recorded ([ADR-022 §3](../adr/ADR-022-standard-trust-and-supply-chain-security.md)).

---

## 5. Runtime-Agnostic Requirement

The registry metadata format is **runtime-agnostic**: pure data, no Go-specific fields, no Go types leaked into the format ([ADR-023 §3](../adr/ADR-023-delivery-lifecycle-standard-registry.md); [Transition Plan §5.10](../planning/ANVIL_V2_TRANSITION_PLAN.md): independent runtime implementations remain structurally possible — a validation, not a v2 requirement).

This means:

- The format is defined solely by this document and the JSON Schema; an implementation in any language that reads and writes documents conforming to the schema is a conforming implementation.
- The format references no engine paths, no engine internals, and no standard content.
- The Go struct set in the Core repository that mirrors this schema is an implementation detail of the Core; it is not part of the format.

---

## 6. Lifecycle of the Format Itself

The format is versioned with the specification version line (§2.3). A breaking change to the format — any change to the document structure, trust semantics, or lifecycle semantics that breaks compatibility — is a governed event that requires an ADR and ships with a Core major version (§2.4; [ADR-024 §3.3](../adr/ADR-024-contract-specification-versioning-and-compatibility-policy.md); [ADR-035 §3.1](../adr/ADR-035-governance-and-identity-reframing-amendments.md)).

---

## 7. Scope Boundaries

This document defines the registry metadata format only. Related concerns of the registry (ADR-023) and the distribution channel (ADR-030) are separate work:

| Not defined here | Where it is defined |
|---|---|
| Registry wire protocol between client and index | EPIC-014 implementation design (TS-014-01-01 §2) |
| Metadata parsing and validation in the registry client | TS-014-01-02 (consumes this format) |
| Client lifecycle-state behavior (warnings, updates, fresh adoption) | TS-014-01-03 |
| Discovery and static index mechanics | ADR-030; TS-014-02-01 |
| Installation/update semantics, offline/bundled install path | ADR-023 §3; EPIC-014 |
| Standard command contract (runtime ↔ standard exchange) | [command-contract.md](command-contract.md) |
| Artifact manifest contract (content identity, embedded manifest) | [artifact-manifest.md](artifact-manifest.md) |
| Engine behavior | The runtime (EPIC-014/EPIC-015) implements this contract; engine internals are not part of the specification |

---

## 8. Traceability

| Section | Source of truth |
|---|---|
| §1 Purpose | ADR-023 §3; PRD-002 §5.7; Transition Plan §5.1, §5.10; ADR-030 §3 |
| §2 Authority, publication, governance | ADR-024 §3; ADR-029 §3; ADR-035 §3; Transition Plan §5.2 |
| §3 Terminology | ADR-021 §3.1–§3.2; ADR-022 §3; ADR-023 §3; ADR-024 §3.1; ADR-027 §3; ADR-030 §3 |
| §4 Document structure | ADR-021 §3.2; ADR-022 §3; ADR-023 §3; ADR-024 §3.1; ADR-027 §3; ADR-030 §3; PRD-002 §5.7–§5.8 |
| §5 Runtime-agnostic requirement | ADR-023 §3; ADR-029 §3; Transition Plan §5.10 |
| §6 Lifecycle of the format | ADR-024 §3.1, §3.3; ADR-035 §3.1 |
| §7 Scope boundaries | ADR-023 §3; ADR-030 §3; TS-014-01-02; TS-014-01-03 |
| §8 Traceability | — |

---

*End of registry-metadata.md*
