// Package registry defines the Anvil Runtime's Go mirror of the registry
// metadata format (docs/specification-corpus/registry-metadata.schema.json;
// TS-014-01-01).
//
// The registry metadata format is runtime-agnostic by design (ADR-023 §3,
// Transition Plan §5.10): the JSON Schema is the machine-readable authority
// (ADR-029 §3) and independent runtime implementations consume the format
// directly. These types are the Core's mirror of that format for runtime
// use — they are an implementation detail of the Core, not part of the
// format. The json tags reproduce the schema field names exactly; no Go
// type leaks into the format.
//
// Parsing and validation of metadata documents is the registry client's
// responsibility (TS-014-01-02); this package defines the data model.
//
// Reference: TS-014-01-01, ADR-022, ADR-023, ADR-030
package registry

// SchemaID is the $id of the registry metadata schema, corpus version
// 1.0.0. Metadata documents may declare their target schema in the
// optional $schema field.
//
// Reference: TS-014-01-01, ADR-024 §3.1, ADR-029 §3
const SchemaID = "urn:anvil:spec:registry-metadata:1.0.0"

// Lifecycle state machine values. The schema enumerates exactly these
// three values (lowercase); the capitalized prose forms in the ADRs
// (Published/Deprecated/Retired) denote the same states (ADR-023 §3,
// ADR-027 §3).
const (
	// LifecycleStatePublished marks a standard discoverable, installable,
	// and validated against the declared contract version (ADR-027 §3).
	LifecycleStatePublished = "published"

	// LifecycleStateDeprecated marks a standard installable with warning,
	// receiving no updates, carrying an announced removal date
	// (ADR-027 §3).
	LifecycleStateDeprecated = "deprecated"

	// LifecycleStateRetired marks a standard removed from the registry
	// with a documented migration path (ADR-027 §3).
	LifecycleStateRetired = "retired"
)

// Trust baseline constants (ADR-022 §7 defers the mechanism to
// implementation design; fixed by TS-014-01-01 per PM decision D-01).
const (
	// DigestAlgorithmSHA256 is the only content digest algorithm the
	// trust baseline supports: integrity is a SHA-256 content digest.
	DigestAlgorithmSHA256 = "sha-256"

	// DigestEncodingBase16 is the hex digest encoding, the conventional
	// default.
	DigestEncodingBase16 = "base16"

	// DigestEncodingBase32 is the base32 digest encoding.
	DigestEncodingBase32 = "base32"

	// DigestEncodingBase64 is the base64 digest encoding.
	DigestEncodingBase64 = "base64"

	// AttestationAlgorithmEd25519 is the only attestation algorithm the
	// trust baseline supports: publisher attestation is an Ed25519
	// signature over the canonical attestation payload (id, version,
	// content digests).
	AttestationAlgorithmEd25519 = "ed25519"

	// DistributionTypeGitHubReleases is the only distribution channel
	// pattern the schema supports: standard content follows the existing
	// GitHub-releases release workflow (ADR-030 §3, §5).
	DistributionTypeGitHubReleases = "github-releases"
)

// Metadata describes one release of one delivery lifecycle standard: the
// distribution unit of the standard registry (ADR-023 §3). One document,
// one release — identity, version, declared contract version, capability
// declaration, distribution location, lifecycle state, and trust fields.
//
// Every section is required: each one is needed at adoption — identity and
// version (idempotent, pinned installation), contract version and
// capability (compatibility validation), distribution location (content
// resolution), lifecycle state (availability semantics), and trust fields
// (integrity and attestation, ADR-022).
//
// Reference: TS-014-01-01 §4, ADR-023 §3
type Metadata struct {
	// Schema is the optional declaration of the schema this document
	// targets (the $id of registry-metadata.schema.json).
	Schema string `json:"$schema,omitempty"`

	// Title is an optional human-readable title of the document.
	Title string `json:"title,omitempty"`

	// Description is an optional human-readable description of the
	// document.
	Description string `json:"description,omitempty"`

	// ID is the standard identity: the stable identifier of the
	// delivery lifecycle standard this release belongs to, stable
	// across releases. The identity half of the installation
	// idempotency key "standard identity plus version" (ADR-023 §3).
	ID string `json:"id"`

	// Version is the release version, semver (major.minor.patch).
	// Standards are independently versioned (ADR-021 §3.4). The second
	// half of the installation idempotency key (ADR-023 §3); adoptions
	// pin this version (ADR-022 §3).
	Version string `json:"version"`

	// ContractVersion is the declared contract version of the delivery
	// lifecycle specification this release targets, semver; the major
	// version is the compatibility unit (ADR-024 §3.1). A release that
	// declares an unsupported contract version is rejected at adoption
	// (PRD-002 §5.8; ADR-023 §3).
	ContractVersion string `json:"contractVersion"`

	// Capability is the capability declaration: the framework-version
	// support scope of this release (ADR-021 §3.2; PRD-002 §5.8).
	Capability Capability `json:"capability"`

	// Distribution is the distribution location of the release content
	// on the standard's own release channel (ADR-030 §3).
	Distribution Distribution `json:"distribution"`

	// Lifecycle is the lifecycle state of the release (ADR-023 §3,
	// ADR-027 §3).
	Lifecycle Lifecycle `json:"lifecycle"`

	// Trust carries the trust fields: content digest(s) and publisher
	// attestation (ADR-022 §3).
	Trust Trust `json:"trust"`

	// Skills declares the optional additive skills section (TS-021-04;
	// ADR-037 D2): the standard's per-skill assets. Each entry declares a
	// skill name, version, and the release asset carrying the skill
	// content; the asset is covered by an attested named digest entry in
	// Trust.ContentDigests (TS-014-04-04). The section is additive-only
	// and optional — a release without it behaves exactly as before, and
	// older parsers tolerate it as an unknown optional section within the
	// deprecation window (forward-compat decision, registry-metadata.md
	// §4.8).
	Skills []Skill `json:"skills,omitempty"`
}

// Capability is the capability declaration of a release: the declared
// compatibility surface. The contract version is declared at the document
// top level (Metadata.ContractVersion); this object declares the
// framework-version support scope (ADR-021 §3.2).
type Capability struct {
	// FrameworkVersion is the set of framework versions this release
	// supports, each semver. At least one is required; a release that
	// omits or malforms the scope is rejected at adoption (ADR-023 §3;
	// PRD-002 §5.8).
	FrameworkVersion []string `json:"frameworkVersion"`
}

// Distribution is the distribution location of the release content
// (ADR-030 §3): the registry is distribution metadata, not content
// hosting; content is resolved from the standard's own release channel.
type Distribution struct {
	// Type is the distribution channel pattern; the only supported
	// value is "github-releases" (ADR-030 §3, §5).
	Type string `json:"type"`

	// Location is the URL of the release content on the declared
	// channel. It is a distribution address, not an identity
	// (Manifesto §3.4; ADR-030 §3).
	Location string `json:"location"`
}

// Lifecycle is the lifecycle state of a standard release in the registry
// (ADR-023 §3, ADR-027 §3). The schema reflects the governed states;
// client-side lifecycle behavior is the registry client's responsibility
// (TS-014-01-03).
type Lifecycle struct {
	// State is the lifecycle state machine value: published,
	// deprecated, or retired.
	State string `json:"state"`

	// RemovalDate is the announced removal date of a deprecated
	// standard, ISO 8601 date-time. Optional: meaningful for the
	// deprecated state and SHOULD be present once the removal date is
	// announced (ADR-023 §3, ADR-027 §3; PM decision D-03).
	RemovalDate string `json:"removalDate,omitempty"`
}

// Trust carries the trust fields of a release (ADR-022 §3): integrity
// verification material and publisher attestation, present from day one.
// The trust model applies equally to first-party and community standards;
// there is no privileged path (ADR-022 §3).
type Trust struct {
	// ContentDigests is the digest set of the release: the release
	// content digests plus, optionally, the attestation-bound digests
	// of named release assets (e.g. the adapter binaries of the same
	// release, TS-014-04-04). At least one entry is required (ADR-022
	// §3; ADR-023 §3: offline/bundled installs carry the same integrity
	// verification); entries without a name are content digests of the
	// release content resolved from Distribution.Location.
	ContentDigests []ContentDigest `json:"contentDigests"`

	// Attestation is the publisher attestation: an Ed25519 signature
	// over the canonical attestation payload (id, version, content
	// digests) with the publisher's verification public key (ADR-022
	// §3; PM decision D-01).
	Attestation Attestation `json:"attestation"`
}

// ContentDigest is one integrity digest entry: algorithm, encoding, and
// value over the release content resolved from Distribution.Location, or
// over a named release asset (TS-014-04-04). The digest is the integrity
// claim; verification recomputes the hash from the resolved content and
// compares — a claim is not evidence (Manifesto §3.2, §3.4).
type ContentDigest struct {
	// Algorithm is the hash algorithm; the trust baseline fixes
	// "sha-256" (PM decision D-01).
	Algorithm string `json:"algorithm"`

	// Encoding is the digest value encoding: base16, base32, or base64.
	Encoding string `json:"encoding"`

	// Digest is the SHA-256 digest in the declared encoding; must be
	// non-empty.
	Digest string `json:"digest"`

	// Name binds the entry to a named release asset of the SAME release
	// (e.g. the adapter binary "anvil-adapter-laravel-linux-amd64",
	// TS-014-04-04). Absent for content digests of the release content.
	// Every entry — named or not — is covered by the publisher
	// attestation, and the name itself is SIGNED material: the canonical
	// payload prefixes each named entry's digest bytes with
	// utf8(name) || 0x00 (security review F-2), so a name can be neither
	// stripped nor renamed across assets without invalidating the
	// attestation; named entries are additionally
	// verified against the downloaded asset at install, closing the
	// same-channel-checksum trust gap (TS-016-04-01 §6 accepted risk
	// 1). The pattern restricts names to safe asset identifiers
	// (^[a-z0-9][a-z0-9-]*$), so a name can never escape the release
	// channel as a path component.
	Name string `json:"name,omitempty"`
}

// Attestation is the publisher attestation of a release (ADR-022 §3):
// sufficient to establish origin; publishing with sufficient attestation
// is a standard responsibility (ADR-022 §5.6). The signature binds the
// release claims together with the content digest(s) — not over content
// alone — so it cannot be detached and replayed against a different
// release of the same content. The signed payload is the canonical,
// byte-level composition (registry-metadata.md §4.7; security review
// F-2):
//
//	utf8(id) || 0x00 || utf8(version) || 0x00 ||
//	concat(entry bytes in contentDigests array order)
//
// where 0x00 is a single NUL byte, utf8(x) is the UTF-8 encoding of field
// x, and each entry contributes its decoded digest bytes (base16 =
// lowercase hex, base32 = RFC-4648, base64 = RFC-4648 standard with
// padding) prefixed by utf8(name) || 0x00 when the entry carries a name —
// the asset binding is SIGNED, so a name can be neither stripped (forcing
// a checksum fallback) nor renamed across assets without invalidating the
// attestation. Consumers verify the signature over exactly these bytes —
// byte-for-byte; any deviation in the composition invalidates the
// attestation. Releases predating binary attestation carry no named
// entries and compose byte-identically to the pre-F-2 payload. Key
// rotation is out of scope for this schema version: a single verification
// key, no key identifiers (PM decision D-01).
type Attestation struct {
	// Algorithm is the signature algorithm; the trust baseline fixes
	// "ed25519" (PM decision D-01).
	Algorithm string `json:"algorithm"`

	// Signature is the Ed25519 signature over the canonical attestation
	// payload (utf8(id) || 0x00 || utf8(version) || 0x00 || concat(entry
	// bytes in ContentDigests array order), each named entry contributing
	// utf8(name) || 0x00 || decoded digest bytes), base64-encoded
	// (RFC-4648 standard with padding); must be non-empty.
	Signature string `json:"signature"`

	// PublicKey is the publisher's Ed25519 verification public key,
	// base64-encoded (RFC-4648 standard with padding); must be
	// non-empty.
	PublicKey string `json:"publicKey"`
}

// Skill is one declaration in the optional additive skills section of a
// registry metadata document (TS-021-04; ADR-037 D2): a standard's
// per-skill asset. The skill content is distributed as a named release
// asset of the same release (bound to a safe asset identifier such as
// anvil-skill-overview-1-0-0), declared here and covered by an attested
// named digest entry in Trust.ContentDigests (TS-014-04-04) — the skill
// install gate verifies the downloaded asset against that digest
// (ADR-037 D4).
//
// The section is additive-only and optional: a release without skills[]
// behaves exactly as before, and older parsers tolerate the section as an
// unknown-but-optional addition within the deprecation window
// (forward-compat decision, registry-metadata.md §4.8). The declaration
// shape is strict — malformed declarations (invalid name, missing asset,
// invalid or unbound digest) are rejected with actionable errors.
type Skill struct {
	// Name is the skill name: the install target of anvil skill install
	// and the namespace component of skills/<standard-id>/<name>
	// (ADR-037 §7). Safe identifier only (^[a-z0-9][a-z0-9-]*$), at most
	// 64 characters — the corpus id convention; names are unique within
	// one release.
	Name string `json:"name"`

	// Version is the skill version, semver (major.minor.patch) without
	// leading zeros. Skills are versioned content — the version is part
	// of the per-skill asset identifier (anvil-skill-<name>-<version>,
	// dots normalized to hyphens) and the recorded identity in the
	// installed-skills store.
	Version string `json:"version"`

	// Asset is the safe identifier of the release asset carrying the
	// skill content (e.g. anvil-skill-overview-1-0-0; the physical file,
	// e.g. anvil-skill-<name>-<version>.tar.gz, is bound to the
	// identifier by the release pipeline). Safe asset identifier
	// (^[a-z0-9][a-z0-9-]*$), at most 128 characters, and covered by an
	// attested named digest entry in Trust.ContentDigests — the parser
	// enforces the binding so an undeclared or unbound asset is rejected
	// (ADR-037 D2; TS-014-04-04).
	Asset string `json:"asset"`

	// Description is an optional human-readable description of the skill
	// (what the agent gains by loading it). Advisory annotation; no
	// validation semantics beyond being a string.
	Description string `json:"description,omitempty"`
}
