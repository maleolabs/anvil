package skillbundle

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SKILL.md frontmatter validation and provenance injection (TS-021-01;
// ADR-037 D1/D10; agentskills.io).
//
// An Anvil skill's SKILL.md carries a YAML frontmatter block delimited by
// '---' lines, restricted to the portable Agent Skills fields
// (agentskills.io): name, description, license, compatibility, metadata,
// allowed-tools. Agent-specific frontmatter fields (e.g. Claude Code's
// "context") are rejected so one artifact stays portable across agents
// (ADR-037 D1).
//
// Provenance. The provenance header "source: <standard-id> <version>"
// (ADR-037 D10) is NOT required at author time and is NOT a portable
// field. ParseFrontmatter therefore rejects a literal "source" frontmatter
// field as non-portable, and the extractor injects the provenance header
// into the installed copy as a YAML comment — "# source: <standard-id>
// <version>" — inside the frontmatter, so the installed SKILL.md stays
// portable (a comment is not a field) while carrying the provenance
// header (ADR-037 D10).

// SkillMarkdownFileName is the canonical name of the skill description
// file inside the content root (agentskills.io: a skill is a folder named
// after the skill containing SKILL.md).
const SkillMarkdownFileName = "SKILL.md"

// portableFrontmatterFields are the only frontmatter fields an Anvil
// skill may declare (agentskills.io; ADR-037 D1).
var portableFrontmatterFields = []string{
	"name",
	"description",
	"license",
	"compatibility",
	"metadata",
	"allowed-tools",
}

// Frontmatter is the validated frontmatter of one SKILL.md. Only the
// portable fields are surfaced; Raw carries the full validated mapping
// (portable fields only) for consumers that need the optional structured
// fields.
type Frontmatter struct {
	// Name is the skill name, matching the bundle manifest name.
	Name string

	// Description is the human-readable description.
	Description string

	// License is the optional SPDX license expression.
	License string

	// AllowedTools lists the optional allowed-tools entries.
	AllowedTools []string

	// Raw is the full validated frontmatter mapping (portable fields
	// only), decoded as YAML values.
	Raw map[string]any
}

// FrontmatterValidationError identifies one rejection of a SKILL.md
// frontmatter: the offending field and an actionable message.
type FrontmatterValidationError struct {
	// Field is the frontmatter field the rejection concerns.
	Field string

	// Message is a human-readable, actionable explanation.
	Message string
}

// Error implements the error interface.
func (e *FrontmatterValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// FrontmatterError reports that a SKILL.md frontmatter was rejected. It
// aggregates every rejection so the caller can fix all problems in one
// pass.
type FrontmatterError struct {
	// Errors lists every rejection found in the frontmatter.
	Errors []FrontmatterValidationError
}

// Error implements the error interface.
func (e *FrontmatterError) Error() string {
	if len(e.Errors) == 0 {
		return "SKILL.md frontmatter is invalid"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "SKILL.md frontmatter rejected (%d problem%s):", len(e.Errors), plural(len(e.Errors)))
	for _, fe := range e.Errors {
		sb.WriteString("\n  - ")
		sb.WriteString(fe.Error())
	}
	return sb.String()
}

// Is allows errors.Is matching for FrontmatterError: any *FrontmatterError
// value matches the target type.
func (e *FrontmatterError) Is(target error) bool {
	_, ok := target.(*FrontmatterError)
	return ok
}

// ParseFrontmatter parses and validates the frontmatter of one SKILL.md
// document against the portable Agent Skills fields (agentskills.io;
// ADR-037 D1). It returns the validated frontmatter or a *FrontmatterError
// listing every problem, all actionable.
//
// Validation surface (skill-bundle-format.md §5):
//
//   - the document must open with a '---' delimiter line and close with a
//     '---' delimiter line (the frontmatter block is between them);
//   - the block must decode as a YAML mapping;
//   - only the portable fields are allowed; any other field is rejected
//     (agent-specific fields are forbidden in Anvil-distributed skills);
//   - name is required, a string matching ^[a-z0-9][a-z0-9-]*$ (the
//     manifest name match is enforced by the bundle flow, not here);
//   - description is required and non-empty;
//   - license is an optional string; compatibility and metadata are
//     optional mappings; allowed-tools is an optional sequence of strings.
//
// The provenance header is not required here: it is injected at install
// time (ADR-037 D10; skill-bundle-format.md §5.4).
func ParseFrontmatter(skillMD []byte) (*Frontmatter, error) {
	contentStart, contentEnd, _, err := frontmatterBlock(skillMD)
	if err != nil {
		return nil, &FrontmatterError{Errors: []FrontmatterValidationError{{Field: "document", Message: err.Error()}}}
	}
	content := skillMD[contentStart:contentEnd]

	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, &FrontmatterError{Errors: []FrontmatterValidationError{{
			Field:   "document",
			Message: fmt.Sprintf("the frontmatter block is not decodable YAML: %v", err),
		}}}
	}

	// An empty block (no YAML at all — whitespace or comments only, or an
	// explicit YAML null) cannot declare the required fields; it is
	// reported as missing name and description, the actionable shape.
	if doc.Kind != yaml.DocumentNode || doc.Content == nil || doc.Content[0] == nil {
		return nil, frontmatterRequiredMissing()
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode && !isNullScalar(root) {
		return nil, &FrontmatterError{Errors: []FrontmatterValidationError{{
			Field:   "document",
			Message: fmt.Sprintf("the frontmatter block must be a YAML mapping, found %s", nodeKindName(root.Kind)),
		}}}
	}
	if root.Kind != yaml.MappingNode {
		return nil, frontmatterRequiredMissing()
	}

	fm := &Frontmatter{Raw: make(map[string]any)}
	var errs []FrontmatterValidationError

	// A mapping node's Content alternates key, value, key, value...
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valueNode := root.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			errs = append(errs, FrontmatterValidationError{
				Field:   "document",
				Message: "frontmatter keys must be strings",
			})
			continue
		}
		field := keyNode.Value
		if field == "" {
			errs = append(errs, FrontmatterValidationError{
				Field:   "document",
				Message: "frontmatter contains an empty mapping key — a portable frontmatter declares named fields only",
			})
			continue
		}

		switch field {
		case "name":
			if !isStringScalar(valueNode) {
				errs = append(errs, frontmatterTypeError(field, "a string", valueNode))
				continue
			}
			fm.Name = valueNode.Value
			if strings.TrimSpace(fm.Name) == "" {
				errs = append(errs, FrontmatterValidationError{Field: field, Message: "must not be empty"})
				continue
			}
			if !reName.MatchString(fm.Name) {
				errs = append(errs, FrontmatterValidationError{
					Field:   field,
					Message: fmt.Sprintf("does not match the skill name convention: lowercase alphanumeric with hyphens (^[a-z0-9][a-z0-9-]*$), found %q", fm.Name),
				})
			}
		case "description":
			if !isStringScalar(valueNode) {
				errs = append(errs, frontmatterTypeError(field, "a string", valueNode))
				continue
			}
			fm.Description = valueNode.Value
			if strings.TrimSpace(fm.Description) == "" {
				errs = append(errs, FrontmatterValidationError{Field: field, Message: "must not be empty"})
			}
		case "license":
			if !isStringScalar(valueNode) {
				errs = append(errs, frontmatterTypeError(field, "a string", valueNode))
				continue
			}
			fm.License = valueNode.Value
		case "allowed-tools":
			tools, ok := stringSequence(valueNode)
			if !ok {
				errs = append(errs, frontmatterTypeError(field, "a sequence of strings", valueNode))
				continue
			}
			fm.AllowedTools = tools
		case "compatibility":
			if valueNode.Kind != yaml.MappingNode && !isNullScalar(valueNode) {
				errs = append(errs, frontmatterTypeError(field, "a mapping", valueNode))
				continue
			}
		case "metadata":
			if valueNode.Kind != yaml.MappingNode && !isNullScalar(valueNode) {
				errs = append(errs, frontmatterTypeError(field, "a mapping", valueNode))
				continue
			}
		default:
			errs = append(errs, FrontmatterValidationError{
				Field: field,
				Message: fmt.Sprintf("is not a portable Agent Skills field (agentskills.io: %s); agent-specific frontmatter fields are rejected for Anvil-distributed skills so one artifact works across agents (ADR-037 D1)",
					strings.Join(portableFrontmatterFields, ", ")),
			})
			continue
		}

		// Decode the accepted value into Raw (portable fields only). A
		// decode failure here is impossible for the shapes validated
		// above, so the result is ignored.
		var decoded any
		_ = valueNode.Decode(&decoded)
		fm.Raw[field] = decoded
	}

	// Required fields (§5.1): name and description.
	if fm.Name == "" {
		errs = append(errs, FrontmatterValidationError{
			Field:   "name",
			Message: "required field is missing (agentskills.io; skill-bundle-format.md §5.1)",
		})
	}
	if fm.Description == "" {
		errs = append(errs, FrontmatterValidationError{
			Field:   "description",
			Message: "required field is missing (agentskills.io; skill-bundle-format.md §5.1)",
		})
	}

	if len(errs) > 0 {
		return nil, &FrontmatterError{Errors: errs}
	}
	return fm, nil
}

// frontmatterRequiredMissing builds the rejection for a frontmatter that
// declares none of the required fields.
func frontmatterRequiredMissing() *FrontmatterError {
	return &FrontmatterError{Errors: []FrontmatterValidationError{
		{Field: "name", Message: "required field is missing (agentskills.io; skill-bundle-format.md §5.1)"},
		{Field: "description", Message: "required field is missing (agentskills.io; skill-bundle-format.md §5.1)"},
	}}
}

// frontmatterTypeError builds a type rejection for a frontmatter field.
func frontmatterTypeError(field, want string, node *yaml.Node) FrontmatterValidationError {
	return FrontmatterValidationError{
		Field:   field,
		Message: fmt.Sprintf("must be %s, found %s", want, nodeKindName(node.Kind)),
	}
}

// nodeKindName renders a yaml.Node kind for error messages.
func nodeKindName(kind yaml.Kind) string {
	switch kind {
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.MappingNode:
		return "a mapping"
	case yaml.SequenceNode:
		return "a sequence"
	default:
		return "an empty value"
	}
}

// isStringScalar reports whether the node is a string-typed scalar.
func isStringScalar(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && node.Tag == "!!str"
}

// isNullScalar reports whether the node is the YAML null scalar (an
// explicitly null optional field).
func isNullScalar(node *yaml.Node) bool {
	return node.Kind == yaml.ScalarNode && (node.Tag == "!!null" || (node.Tag == "" && node.Value == ""))
}

// stringSequence decodes a sequence-of-strings value, reporting whether
// the node has that exact shape.
func stringSequence(node *yaml.Node) ([]string, bool) {
	if node.Kind != yaml.SequenceNode {
		return nil, false
	}
	out := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if !isStringScalar(item) {
			return nil, false
		}
		out = append(out, item.Value)
	}
	return out, true
}

// frontmatterBlock locates the frontmatter block of a SKILL.md document
// and returns the byte offsets of its content: [contentStart,
// contentEnd) is the YAML between the delimiters, and afterEnd is the
// offset just past the closing delimiter line (including its line
// ending). The leading delimiter must be the first line; the closing
// delimiter is the next line whose content is exactly "---". Both CRLF
// and LF line endings are accepted; the returned offsets refer to the
// original bytes.
func frontmatterBlock(doc []byte) (contentStart, contentEnd, afterEnd int, err error) {
	lineStart := 0
	line, lineEnd, ok := nextLine(doc, lineStart)
	if !ok {
		return 0, 0, 0, fmt.Errorf("the document must open with a '---' frontmatter delimiter line, found nothing (skill-bundle-format.md §5)")
	}
	if !isDelimiter(line) {
		return 0, 0, 0, fmt.Errorf("the document must open with a '---' frontmatter delimiter line, found %q (skill-bundle-format.md §5)", string(line))
	}

	contentStart = lineEnd
	pos := lineEnd
	for {
		line, lineEnd, ok = nextLine(doc, pos)
		if !ok {
			return 0, 0, 0, fmt.Errorf("the frontmatter block has no closing '---' delimiter line (skill-bundle-format.md §5)")
		}
		if isDelimiter(line) {
			return contentStart, pos, lineEnd, nil
		}
		pos = lineEnd
	}
}

// nextLine returns the next line starting at pos (excluding its line
// ending; a trailing '\r' is stripped so CRLF documents are handled) and
// the offset just past the line ending; ok is false at EOF with no
// further line. A final line without a line ending is still a line.
func nextLine(doc []byte, pos int) (line []byte, end int, ok bool) {
	if pos >= len(doc) {
		return nil, pos, false
	}
	start := pos
	for pos < len(doc) && doc[pos] != '\n' {
		pos++
	}
	if pos >= len(doc) {
		return stripCR(doc[start:]), pos, true
	}
	return stripCR(doc[start:pos]), pos + 1, true
}

// stripCR removes a trailing carriage return (CRLF documents).
func stripCR(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\r' {
		return b[:len(b)-1]
	}
	return b
}

// isDelimiter reports whether line is exactly "---" (the frontmatter
// delimiter; CR already stripped by nextLine since lines end at '\n').
func isDelimiter(line []byte) bool {
	return len(line) == 3 && line[0] == '-' && line[1] == '-' && line[2] == '-'
}

// provenanceCommentPrefix is the prefix of the provenance comment line
// injected into the frontmatter of installed SKILL.md files (ADR-037
// D10).
const provenanceCommentPrefix = "# source:"

// InjectProvenance returns a copy of skillMD whose frontmatter carries the
// provenance header "# source: <source> <version>" as a YAML comment
// (ADR-037 D10; skill-bundle-format.md §5.4). The header is injected at
// install time, after frontmatter validation — it is not a portable field
// and never required at author time.
//
// Injection is idempotent: if the frontmatter already carries a "#
// source:" comment line, its value is replaced; otherwise the header is
// inserted as the first line inside the frontmatter block. Everything
// else in the document is preserved byte-for-byte.
//
// An invalid source or version is rejected (never injected): a provenance
// header must be well-formed to be meaningful.
func InjectProvenance(skillMD []byte, source, version string) ([]byte, error) {
	if !ValidateName(source) {
		return nil, fmt.Errorf("cannot inject provenance: source %q is not a valid standard-id (^[a-z0-9][a-z0-9-]*$)", source)
	}
	if !ValidateVersion(version) {
		return nil, fmt.Errorf("cannot inject provenance: version %q is not a valid semver without leading zeros", version)
	}

	contentStart, contentEnd, _, err := frontmatterBlock(skillMD)
	if err != nil {
		return nil, err
	}
	content := skillMD[contentStart:contentEnd]
	line := []byte(provenanceCommentPrefix + " " + source + " " + version)

	// Replace an existing provenance comment line, if any.
	pos := contentStart
	for {
		l, lineEnd, ok := nextLine(skillMD, pos)
		if !ok || lineEnd > contentEnd {
			break
		}
		if isProvenanceComment(l) {
			// Replace the whole line with the canonical header, preserving
			// the original line terminator ("\n" or "\r\n"): dropping it
			// would swallow the following line (e.g. the closing '---'
			// delimiter) and silently corrupt the installed SKILL.md. l is
			// the CR-stripped line, so pos+len(l) points at its original
			// terminator.
			out := make([]byte, 0, len(skillMD)+len(line)-len(l))
			out = append(out, skillMD[:pos]...)
			out = append(out, line...)
			out = append(out, skillMD[pos+len(l):lineEnd]...)
			out = append(out, skillMD[lineEnd:]...)
			return out, nil
		}
		pos = lineEnd
	}

	// No existing header: insert it as the first line of the frontmatter
	// content, preserving every original byte.
	out := make([]byte, 0, len(skillMD)+len(line)+1)
	out = append(out, skillMD[:contentStart]...)
	out = append(out, line...)
	out = append(out, '\n')
	out = append(out, content...)
	out = append(out, skillMD[contentEnd:]...)
	return out, nil
}

// isProvenanceComment reports whether line is a "# source:" comment
// (allowing leading whitespace, as YAML permits indented comments).
func isProvenanceComment(line []byte) bool {
	trimmed := strings.TrimLeft(string(line), " \t")
	return strings.HasPrefix(trimmed, provenanceCommentPrefix)
}
