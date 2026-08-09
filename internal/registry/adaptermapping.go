// Adapter-to-standard mapping consumption (TS-017-01-02, T-004).
//
// The authoritative mapping from installed v1.x adapters to delivery
// lifecycle standards is the maintained planning artifact
// docs/planning/ANVIL_V2_ADAPTER_STANDARD_MAPPING.md (TS-017-01-01,
// ANVIL_V2_ADAPTER_STANDARD_MAPPING §1): the §3 table is the machine
// contract (§7). The installed-adapter recognition logic consumes this
// artifact as its single source — standard identity is NEVER hard-coded
// in consuming code (§7 "No hard-coding"; a row is added in the artifact
// when a new first-party standard is created, §8, and consumers pick it
// up without a Core change).
//
// The §7 consumption contract this loader implements:
//
//   - the header row of the §3 table is the field contract: the stable
//     column set adapter_name, adapter_executable, adapter_source,
//     standard_id, standard_repository, standard_executable, framework,
//     version_relationship, contract_version. Renaming a column is a
//     breaking change for consumers (§7), so the loader requires the
//     header to match exactly;
//   - one row per first-party v1.x adapter; adapter_name and
//     adapter_executable are unique per row and serve as the lookup
//     keys (duplicate keys make the table unusable for recognition and
//     are rejected);
//   - multi-value cells are ';'-separated (adapter_source is the
//     multi-value column today); cell values never contain '|' or
//     newlines (structural: parsing is line- and pipe-based, so a cell
//     containing either breaks the table shape and is rejected);
//   - row order is not part of the contract: lookups are key-based and
//     rows are exposed sorted by adapter_name.
//
// Loading is purely local and mirrors the compatibility matrix reader
// (LoadCompatibilityMatrix, adopt.go — the established corpus
// consumption pattern, ADR-029 §3): the artifact is read from disk at
// runtime, never hardcoded, so the runtime and the maintained mapping
// cannot drift. The default location is the artifact relative to the
// working directory (the corpus is co-located with the engine in the
// repository, ADR-029 §5.2); ANVIL_ADAPTER_STANDARD_MAPPING points
// operators at the artifact from elsewhere. A mapping that cannot be
// read — missing, oversize, or structurally invalid — is an actionable
// error naming the file and the fix; recognition is never silently
// skipped (explicit over implicit, Manifesto §3.10).
//
// Scope. The table covers the first-party v1.x adapters only; the v1.x
// closed set is not closed (third-party anvil-adapter-<name> binaries
// are discoverable), and third-party adapters have no row here and are
// out of scope for the first-party mapping (§7).
//
// Reference: TS-017-01-01 §7, TS-017-01-02, ADR-028 §3, §12.3,
// ADR-029 §3, ADR-025 §3
package registry

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
)

// EnvAdapterStandardMapping is the environment variable pointing at the
// adapter-to-standard mapping artifact file (the maintained table,
// docs/planning/ANVIL_V2_ADAPTER_STANDARD_MAPPING.md). It mirrors the
// compatibility matrix convention (EnvCompatibilityMatrix, adopt.go):
// the artifact is co-located with the engine in the repository, so
// running the engine from the repository root locates it without
// configuration; operators running the engine from elsewhere point it
// at the artifact file.
const EnvAdapterStandardMapping = "ANVIL_ADAPTER_STANDARD_MAPPING"

// DefaultAdapterMappingRelativePath is the adapter-to-standard mapping
// artifact path relative to the repository root: the maintained table
// that is the authoritative adapter → standard mapping for installed
// v1.x adapters (docs/planning/ANVIL_V2_ADAPTER_STANDARD_MAPPING.md,
// TS-017-01-01 §7).
const DefaultAdapterMappingRelativePath = "docs/planning/ANVIL_V2_ADAPTER_STANDARD_MAPPING.md"

// MaxAdapterMappingSize caps the size of the mapping artifact file
// (1 MiB). The table holds one row per first-party adapter — a file
// beyond the cap is a broken artifact and fails load with a precise,
// actionable error instead of unbounded memory use (mirrors
// MaxCompatibilityMatrixSize).
const MaxAdapterMappingSize = 1 << 20

// adapterMappingColumns is the field contract of the §3 table header
// row (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7): the stable column set in
// order. The header row must match exactly — renaming a column is a
// breaking change for consumers and is rejected by the loader.
var adapterMappingColumns = []string{
	"adapter_name",
	"adapter_executable",
	"adapter_source",
	"standard_id",
	"standard_repository",
	"standard_executable",
	"framework",
	"version_relationship",
	"contract_version",
}

// Sentinel errors for mapping loading. Consumers match them with
// errors.Is on the wrapped errors returned by LoadAdapterMapping.
var (
	// ErrAdapterMappingNotFound reports that the mapping artifact file
	// does not exist.
	ErrAdapterMappingNotFound = errors.New("adapter-to-standard mapping file not found")

	// ErrAdapterMappingInvalid reports that the mapping artifact file
	// exists but does not satisfy the §7 machine consumption contract:
	// the table or its header is missing, a row is malformed, a lookup
	// key is duplicated, or a required cell is empty. The wrapped error
	// names the file, the table line, and the fix.
	ErrAdapterMappingInvalid = errors.New("adapter-to-standard mapping file invalid")
)

// AdapterMappingRow is one row of the §3 mapping table
// (ANVIL_V2_ADAPTER_STANDARD_MAPPING §3, §4): the correspondence between
// one first-party v1.x adapter identity (adapter_name /
// adapter_executable — the lookup keys, §7) and the delivery lifecycle
// standard that carries its content (standard_id / standard_repository /
// standard_executable). Standard identity values are read from the
// maintained artifact — never hard-coded in consuming code (§7).
type AdapterMappingRow struct {
	// AdapterName is the v1.x adapter identifier: the <name> argument
	// of the v1.x CLI surface and the value of project.framework in
	// anvil.yaml (v1.x). Unique per row; a lookup key (§7, §4).
	AdapterName string

	// AdapterExecutable is the v1.x adapter executable name:
	// anvil-adapter-<framework>, resolved on PATH / next to the CLI by
	// closed-set discovery (TS-007-039). Unique per row; a lookup key
	// (§7, §4).
	AdapterExecutable string

	// AdapterSource is the v1.x monorepo locations that became the
	// standard, ';'-separated in the artifact (§4) — the multi-value
	// column of the table.
	AdapterSource []string

	// StandardID is the registry standard identity:
	// anvil-standard-<framework>, stable across releases of the
	// standard (§4). This is the migration target of the recognition
	// logic.
	StandardID string

	// StandardRepository is the standard's repository under the
	// maleolabs namespace (§4).
	StandardRepository string

	// StandardExecutable is the installed executable of the standard.
	// The non-breaking default preserves the v1.x executable resolution
	// contract (anvil-adapter-<framework>); changing it is a governed
	// breaking event (§4).
	StandardExecutable string

	// Framework is the framework the standard carries — the
	// natural-language anchor of the row (§4).
	Framework string

	// VersionRelationship is the version relationship between the v1.x
	// adapter line and the standard line; the stable enum value
	// "independent-lines" (§4, §5).
	VersionRelationship string

	// ContractVersion is the declared contract version each standard
	// targets. Concrete values are declared by the standard's
	// lifecycle-model contract in the standard repository and are not
	// authored in Core (§4, §6); the cell records that relationship.
	// Contract-version VALIDATION at migration is TS-017-01-03 (T-007)
	// and is not implemented here.
	ContractVersion string
}

// AdapterMapping is the parsed §3 table: the authoritative mapping from
// installed v1.x adapters to delivery lifecycle standards
// (ANVIL_V2_ADAPTER_STANDARD_MAPPING §1, §7). Lookup keys are
// adapter_name and adapter_executable (§7); row order is not part of
// the contract.
type AdapterMapping struct {
	rows         map[string]AdapterMappingRow // by adapter_name (lookup key)
	byExecutable map[string]AdapterMappingRow // by adapter_executable (lookup key)
}

// LoadAdapterMapping reads and validates the adapter-to-standard mapping
// artifact at path (TS-017-01-02; ANVIL_V2_ADAPTER_STANDARD_MAPPING §7):
//
//   - the file must exist (wrapped ErrAdapterMappingNotFound) and be
//     readable;
//   - the file must not exceed MaxAdapterMappingSize;
//   - the file must contain exactly one table whose header row is the
//     field contract (adapterMappingColumns, in order) followed by its
//     separator line — the §3 table; a file without it (or with more
//     than one such table) is a broken artifact (wrapped
//     ErrAdapterMappingInvalid);
//   - every data row must carry exactly one cell per contract column;
//     the required identity cells (adapter_name, adapter_executable,
//     standard_id) must be non-empty; cell values never contain '|' or
//     newlines (structural, enforced by the line- and pipe-based
//     parse);
//   - adapter_name and adapter_executable are unique per row — the
//     lookup keys of §7; a duplicate key makes recognition ambiguous
//     and is rejected;
//   - multi-value cells (';'-separated, adapter_source) are split into
//     the row's slice column.
//
// Loading is purely local: the mapping is read from disk at adoption
// time — standard identity is read, never hardcoded, so the runtime and
// the maintained artifact cannot drift (ADR-029 §3; §7 "No
// hard-coding"). A mapping that cannot be read is an actionable error
// naming the file and the fix; recognition is never silently skipped.
//
// Reference: TS-017-01-01 §7, TS-017-01-02, ADR-029 §3
func LoadAdapterMapping(path string) (*AdapterMapping, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrAdapterMappingNotFound, path)
		}
		return nil, fmt.Errorf("adapter-to-standard mapping: open %s: %w", path, err)
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, MaxAdapterMappingSize+1))
	if err != nil {
		return nil, fmt.Errorf("adapter-to-standard mapping: read %s: %w", path, err)
	}
	if len(raw) > MaxAdapterMappingSize {
		return nil, fmt.Errorf(
			"adapter-to-standard mapping: %s: file exceeds the %d-byte size cap",
			path, MaxAdapterMappingSize)
	}

	rows, err := parseAdapterMappingTable(path, string(raw))
	if err != nil {
		return nil, err
	}

	mapping := &AdapterMapping{
		rows:         make(map[string]AdapterMappingRow, len(rows)),
		byExecutable: make(map[string]AdapterMappingRow, len(rows)),
	}
	for _, row := range rows {
		mapping.rows[row.AdapterName] = row
		mapping.byExecutable[row.AdapterExecutable] = row
	}
	return mapping, nil
}

// ResolveAdapterMappingPath resolves the adapter-to-standard mapping
// artifact path, in order:
//
//  1. the explicit path argument (non-empty);
//  2. the ANVIL_ADAPTER_STANDARD_MAPPING environment variable
//     (non-empty);
//  3. the documented default: the artifact relative to the working
//     directory (DefaultAdapterMappingRelativePath) — the maintained
//     table is co-located with the engine in the repository (ADR-029
//     §5.2), so running the engine from the repository root locates it
//     without configuration.
//
// getenv is injected for testability. This mirrors the compatibility
// matrix path convention (ResolveCompatibilityMatrixPath, adopt.go).
// Loading happens later, at adoption time (LoadAdapterMapping); a
// mapping that cannot be resolved or read is surfaced explicitly — never
// silently defaulted.
func ResolveAdapterMappingPath(explicit string, getenv func(string) string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if value := getenv(EnvAdapterStandardMapping); value != "" {
		return value, nil
	}
	return DefaultAdapterMappingRelativePath, nil
}

// LookupByAdapterName returns the mapping row whose adapter_name is
// name — the recognition lookup key for a project's declared framework
// (project.framework, v1.x; the adapter_name column, §7). The boolean
// reports whether the name is a first-party adapter identity.
func (m *AdapterMapping) LookupByAdapterName(name string) (AdapterMappingRow, bool) {
	if m == nil {
		return AdapterMappingRow{}, false
	}
	row, ok := m.rows[name]
	return row, ok
}

// LookupByAdapterExecutable returns the mapping row whose
// adapter_executable is executable — the recognition lookup key for an
// installed adapter executable name (anvil-adapter-<name>, §7). The
// boolean reports whether the executable is a first-party adapter
// identity.
func (m *AdapterMapping) LookupByAdapterExecutable(executable string) (AdapterMappingRow, bool) {
	if m == nil {
		return AdapterMappingRow{}, false
	}
	row, ok := m.byExecutable[executable]
	return row, ok
}

// Rows returns every mapping row sorted by adapter_name. Row order is
// not part of the §7 contract; the sort makes the set deterministic for
// callers that enumerate it.
func (m *AdapterMapping) Rows() []AdapterMappingRow {
	if m == nil {
		return nil
	}
	names := make([]string, 0, len(m.rows))
	for name := range m.rows {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]AdapterMappingRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, m.rows[name])
	}
	return rows
}

// separatorCellPattern matches a markdown table separator cell
// (---, :---, ---:, :---:).
var separatorCellPattern = regexp.MustCompile(`^:?-+:?$`)

// parseAdapterMappingTable parses the §3 table out of the artifact
// content (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7): it locates the ONE
// table whose header row is the field contract, verifies its separator
// line, and parses every data row into AdapterMappingRow. Any deviation
// from the contract — a missing or duplicated table, a malformed
// separator, a row with the wrong cell count, a duplicated lookup key,
// or an empty required identity cell — is an actionable error naming
// the file, the line, and the fix.
func parseAdapterMappingTable(path, content string) ([]AdapterMappingRow, error) {
	lines := strings.Split(content, "\n")

	// Locate the table header: the line whose pipe-separated cells are
	// exactly the field contract. The artifact contains other markdown
	// tables (metadata, column semantics, traceability); only the exact
	// header identifies the §3 table.
	headerIdx := -1
	for i, line := range lines {
		cells := splitTableCells(line)
		if len(cells) != len(adapterMappingColumns) {
			continue
		}
		match := true
		for j, cell := range cells {
			if cell != adapterMappingColumns[j] {
				match = false
				break
			}
		}
		if match {
			if headerIdx != -1 {
				return nil, fmt.Errorf(
					"%w: %s: more than one table declares the field-contract header row — the mapping table must appear exactly once (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7); check for a duplicated §3 table",
					ErrAdapterMappingInvalid, path)
			}
			headerIdx = i
		}
	}
	if headerIdx == -1 {
		return nil, fmt.Errorf(
			"%w: %s: no mapping table found — expected a table whose header row is the field contract: | %s | (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7); the header row is the machine contract and must match exactly",
			ErrAdapterMappingInvalid, path, strings.Join(adapterMappingColumns, " | "))
	}

	// The line after the header must be the separator line.
	if headerIdx+1 >= len(lines) || !isSeparatorLine(lines[headerIdx+1]) {
		return nil, fmt.Errorf(
			"%w: %s: line %d: the mapping table header (line %d) must be followed by its separator row (|---|---|...|) — a malformed table is a broken artifact (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7)",
			ErrAdapterMappingInvalid, path, headerIdx+2, headerIdx+1)
	}

	// Data rows: every consecutive line that starts a table row after
	// the separator. A cell never contains '|' or a newline — the parse
	// is line- and pipe-based, so either character breaks the cell count
	// or the row shape and is rejected below.
	var rows []AdapterMappingRow
	for i := headerIdx + 2; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || !strings.HasPrefix(trimmed, "|") {
			break // the table ends at the first non-row line
		}
		cells := splitTableCells(lines[i])
		if len(cells) != len(adapterMappingColumns) {
			return nil, fmt.Errorf(
				"%w: %s: line %d: row has %d cell(s), expected %d — every row must carry exactly one cell per contract column; a cell containing '|' or a newline breaks the table shape (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7)",
				ErrAdapterMappingInvalid, path, i+1, len(cells), len(adapterMappingColumns))
		}
		row := AdapterMappingRow{
			AdapterName:         cells[0],
			AdapterExecutable:   cells[1],
			AdapterSource:       splitMultiValueCell(cells[2]),
			StandardID:          cells[3],
			StandardRepository:  cells[4],
			StandardExecutable:  cells[5],
			Framework:           cells[6],
			VersionRelationship: cells[7],
			ContractVersion:     cells[8],
		}
		if err := validateMappingRow(path, i+1, row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}

	// A mapping with no data rows cannot supply the mapping: the table
	// must carry one row per first-party v1.x adapter (§7) — a header
	// with a separator and no rows is a broken artifact, never an empty
	// mapping (fail-closed).
	if len(rows) == 0 {
		return nil, fmt.Errorf(
			"%w: %s: the mapping table declares no data rows — the table must carry one row per first-party v1.x adapter (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7); a table with no rows cannot supply the mapping",
			ErrAdapterMappingInvalid, path)
	}

	// Uniqueness of the lookup keys (ANVIL_V2_ADAPTER_STANDARD_MAPPING
	// §7): adapter_name and adapter_executable are unique per row.
	seenNames := make(map[string]bool, len(rows))
	seenExecutables := make(map[string]bool, len(rows))
	for _, row := range rows {
		if seenNames[row.AdapterName] {
			return nil, fmt.Errorf(
				"%w: %s: adapter_name %q appears in more than one row — adapter_name is a lookup key and must be unique per row (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7)",
				ErrAdapterMappingInvalid, path, row.AdapterName)
		}
		seenNames[row.AdapterName] = true
		if seenExecutables[row.AdapterExecutable] {
			return nil, fmt.Errorf(
				"%w: %s: adapter_executable %q appears in more than one row — adapter_executable is a lookup key and must be unique per row (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7)",
				ErrAdapterMappingInvalid, path, row.AdapterExecutable)
		}
		seenExecutables[row.AdapterExecutable] = true
	}

	return rows, nil
}

// validateMappingRow checks the identity cells a recognition row needs
// (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7): the lookup keys and the
// migration target must be non-empty.
func validateMappingRow(path string, line int, row AdapterMappingRow) error {
	if row.AdapterName == "" {
		return fmt.Errorf(
			"%w: %s: line %d: adapter_name is empty — the lookup key must identify the v1.x adapter (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7)",
			ErrAdapterMappingInvalid, path, line)
	}
	if row.AdapterExecutable == "" {
		return fmt.Errorf(
			"%w: %s: line %d: adapter_executable is empty — the lookup key must name the v1.x adapter executable (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7)",
			ErrAdapterMappingInvalid, path, line)
	}
	if row.StandardID == "" {
		return fmt.Errorf(
			"%w: %s: line %d: standard_id is empty — the migration target must name the delivery lifecycle standard (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7)",
			ErrAdapterMappingInvalid, path, line)
	}
	return nil
}

// splitTableCells splits one markdown table line into its trimmed cell
// values: the line must start and end with '|'; the leading/trailing
// pipes are stripped and the remainder is split on '|'. A line that is
// not a table row returns nil.
func splitTableCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "|"), "|")
	parts := strings.Split(inner, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

// isSeparatorLine reports whether the line is a markdown table
// separator row (every cell is a -/:-/:--/:-:- run).
func isSeparatorLine(line string) bool {
	cells := splitTableCells(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		if !separatorCellPattern.MatchString(cell) {
			return false
		}
	}
	return true
}

// splitMultiValueCell splits a ';'-separated multi-value cell into its
// trimmed values (ANVIL_V2_ADAPTER_STANDARD_MAPPING §7: multi-value
// cells are ';'-separated). Single-value cells yield one element; an
// empty cell yields nil.
func splitMultiValueCell(cell string) []string {
	if cell == "" {
		return nil
	}
	parts := strings.Split(cell, ";")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}
