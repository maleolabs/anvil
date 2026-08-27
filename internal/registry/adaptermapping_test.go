package registry

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// authoritativeMappingPath returns the path of the maintained
// adapter-to-standard mapping artifact relative to this package's test
// working directory (go test runs with the package directory as the
// working directory).
func authoritativeMappingPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "planning", "ANVIL_V2_ADAPTER_STANDARD_MAPPING.md")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("adapter mapping not present (EKA mode) — %v", err)
	}
	return path
}

// TestLoadAdapterMapping_AuthoritativeArtifact verifies the loader
// against the REAL maintained artifact (TS-017-01-01 §7 — the table is
// the authoritative mapping): the §3 table parses into exactly the two
// first-party rows (Laravel, Flutter) with the documented field values,
// and both lookup keys (adapter_name, adapter_executable) resolve both
// rows. This test is the drift guard: standard identity is consumed
// from the artifact, never hard-coded.
func TestLoadAdapterMapping_AuthoritativeArtifact(t *testing.T) {
	mapping, err := LoadAdapterMapping(authoritativeMappingPath(t))
	if err != nil {
		t.Fatalf("LoadAdapterMapping(authoritative artifact) returned error: %v", err)
	}

	rows := mapping.Rows()
	if len(rows) != 2 {
		t.Fatalf("authoritative mapping has %d row(s), want 2 (laravel, flutter)", len(rows))
	}

	// Lookup by adapter_name — the recognition key for a project's
	// declared framework.
	laravel, ok := mapping.LookupByAdapterName("laravel")
	if !ok {
		t.Fatal("LookupByAdapterName(laravel) not found in the authoritative artifact")
	}
	wantLaravel := AdapterMappingRow{
		AdapterName:         "laravel",
		AdapterExecutable:   "anvil-adapter-laravel",
		AdapterSource:       []string{"internal/laravel", "cmd/laravel-adapter"},
		StandardID:          "anvil-standard-laravel",
		StandardRepository:  "maleolabs/anvil-standard-laravel",
		StandardExecutable:  "anvil-adapter-laravel",
		Framework:           "Laravel",
		VersionRelationship: "independent-lines",
		ContractVersion:     "declared by the standard's lifecycle-model contract",
	}
	if !reflect.DeepEqual(laravel, wantLaravel) {
		t.Errorf("laravel row = %+v, want %+v", laravel, wantLaravel)
	}

	flutter, ok := mapping.LookupByAdapterName("flutter")
	if !ok {
		t.Fatal("LookupByAdapterName(flutter) not found in the authoritative artifact")
	}
	if flutter.StandardID != "anvil-standard-flutter" || flutter.AdapterExecutable != "anvil-adapter-flutter" {
		t.Errorf("flutter row = %+v, want standard anvil-standard-flutter with executable anvil-adapter-flutter", flutter)
	}

	// Lookup by adapter_executable — the second §7 lookup key.
	if got, ok := mapping.LookupByAdapterExecutable("anvil-adapter-laravel"); !ok || got.AdapterName != "laravel" {
		t.Errorf("LookupByAdapterExecutable(anvil-adapter-laravel) = (%+v, %v), want (laravel row, true)", got, ok)
	}
	if got, ok := mapping.LookupByAdapterExecutable("anvil-adapter-flutter"); !ok || got.AdapterName != "flutter" {
		t.Errorf("LookupByAdapterExecutable(anvil-adapter-flutter) = (%+v, %v), want (flutter row, true)", got, ok)
	}

	// A name/executable outside the first-party set is not recognized.
	if _, ok := mapping.LookupByAdapterName("rails"); ok {
		t.Error("LookupByAdapterName(rails) should not resolve — third-party adapters have no first-party mapping row (§7)")
	}
	if _, ok := mapping.LookupByAdapterExecutable("anvil-adapter-node"); ok {
		t.Error("LookupByAdapterExecutable(anvil-adapter-node) should not resolve")
	}

	// Rows are exposed sorted by adapter_name (row order is not part of
	// the §7 contract, but the enumeration is deterministic).
	if rows[0].AdapterName != "flutter" || rows[1].AdapterName != "laravel" {
		t.Errorf("Rows() order = %q, %q — want flutter, laravel (sorted by adapter_name)", rows[0].AdapterName, rows[1].AdapterName)
	}
}

// TestLoadAdapterMapping_MissingFile verifies that a missing artifact is
// an actionable wrapped ErrAdapterMappingNotFound — never a silent
// default (mirrors the compatibility matrix convention).
func TestLoadAdapterMapping_MissingFile(t *testing.T) {
	_, err := LoadAdapterMapping(filepath.Join(t.TempDir(), "no-such-mapping.md"))
	if !errors.Is(err, ErrAdapterMappingNotFound) {
		t.Fatalf("error = %v, want wrapped ErrAdapterMappingNotFound", err)
	}
}

// TestLoadAdapterMapping_NoMappingTable verifies that an artifact
// without a table whose header is the field contract is a broken
// artifact: the header row is the machine contract (§7), and a file
// that does not declare it cannot supply the mapping.
func TestLoadAdapterMapping_NoMappingTable(t *testing.T) {
	path := writeMappingFile(t, "# Some doc\n\n| other | columns |\n|---|---|\n| a | b |\n")
	_, err := LoadAdapterMapping(path)
	if !errors.Is(err, ErrAdapterMappingInvalid) {
		t.Fatalf("error = %v, want wrapped ErrAdapterMappingInvalid", err)
	}
	if !strings.Contains(err.Error(), "header row") {
		t.Errorf("error should name the missing header row, got: %v", err)
	}
}

// TestLoadAdapterMapping_DuplicateTableHeader verifies that an artifact
// declaring the field-contract header more than once is rejected: the
// mapping table must appear exactly once.
func TestLoadAdapterMapping_DuplicateTableHeader(t *testing.T) {
	header := "| adapter_name | adapter_executable | adapter_source | standard_id | standard_repository | standard_executable | framework | version_relationship | contract_version |"
	separator := "|---|---|---|---|---|---|---|---|---|"
	row := "| laravel | anvil-adapter-laravel | internal/laravel | anvil-standard-laravel | maleolabs/anvil-standard-laravel | anvil-adapter-laravel | Laravel | independent-lines | declared |"
	content := header + "\n" + separator + "\n" + row + "\n\n" + header + "\n" + separator + "\n" + row + "\n"
	path := writeMappingFile(t, content)
	_, err := LoadAdapterMapping(path)
	if !errors.Is(err, ErrAdapterMappingInvalid) {
		t.Fatalf("error = %v, want wrapped ErrAdapterMappingInvalid", err)
	}
	if !strings.Contains(err.Error(), "more than one table") {
		t.Errorf("error should name the duplicate table, got: %v", err)
	}
}

// TestLoadAdapterMapping_MissingSeparator verifies that a header not
// followed by its separator row is a broken table.
func TestLoadAdapterMapping_MissingSeparator(t *testing.T) {
	header := "| adapter_name | adapter_executable | adapter_source | standard_id | standard_repository | standard_executable | framework | version_relationship | contract_version |"
	row := "| laravel | anvil-adapter-laravel | internal/laravel | anvil-standard-laravel | maleolabs/anvil-standard-laravel | anvil-adapter-laravel | Laravel | independent-lines | declared |"
	path := writeMappingFile(t, header+"\n"+row+"\n")
	_, err := LoadAdapterMapping(path)
	if !errors.Is(err, ErrAdapterMappingInvalid) {
		t.Fatalf("error = %v, want wrapped ErrAdapterMappingInvalid", err)
	}
	if !strings.Contains(err.Error(), "separator") {
		t.Errorf("error should name the missing separator row, got: %v", err)
	}
}

// TestLoadAdapterMapping_MalformedRow verifies that a row with a cell
// count different from the field contract is rejected — a cell
// containing '|' or a newline breaks the table shape and the artifact
// is broken (§7: cell values never contain '|' or newlines).
func TestLoadAdapterMapping_MalformedRow(t *testing.T) {
	header := "| adapter_name | adapter_executable | adapter_source | standard_id | standard_repository | standard_executable | framework | version_relationship | contract_version |"
	separator := "|---|---|---|---|---|---|---|---|---|"
	shortRow := "| laravel | anvil-adapter-laravel | internal/laravel | anvil-standard-laravel | maleolabs/anvil-standard-laravel |"
	path := writeMappingFile(t, header+"\n"+separator+"\n"+shortRow+"\n")
	_, err := LoadAdapterMapping(path)
	if !errors.Is(err, ErrAdapterMappingInvalid) {
		t.Fatalf("error = %v, want wrapped ErrAdapterMappingInvalid", err)
	}
	if !strings.Contains(err.Error(), "cell(s)") {
		t.Errorf("error should name the cell count mismatch, got: %v", err)
	}
}

// TestLoadAdapterMapping_EmptyRequiredCell verifies that a row with an
// empty identity cell (adapter_name, adapter_executable, or standard_id)
// is rejected: the lookup keys and the migration target are required.
func TestLoadAdapterMapping_EmptyRequiredCell(t *testing.T) {
	header := "| adapter_name | adapter_executable | adapter_source | standard_id | standard_repository | standard_executable | framework | version_relationship | contract_version |"
	separator := "|---|---|---|---|---|---|---|---|---|"
	badRow := "|  | anvil-adapter-laravel | internal/laravel | anvil-standard-laravel | maleolabs/anvil-standard-laravel | anvil-adapter-laravel | Laravel | independent-lines | declared |"
	path := writeMappingFile(t, header+"\n"+separator+"\n"+badRow+"\n")
	_, err := LoadAdapterMapping(path)
	if !errors.Is(err, ErrAdapterMappingInvalid) {
		t.Fatalf("error = %v, want wrapped ErrAdapterMappingInvalid", err)
	}
	if !strings.Contains(err.Error(), "adapter_name is empty") {
		t.Errorf("error should name the empty adapter_name, got: %v", err)
	}
}

// TestLoadAdapterMapping_DuplicateLookupKeys verifies that duplicated
// adapter_name or adapter_executable values are rejected: the keys are
// unique per row (§7) — a duplicate makes recognition ambiguous.
func TestLoadAdapterMapping_DuplicateLookupKeys(t *testing.T) {
	header := "| adapter_name | adapter_executable | adapter_source | standard_id | standard_repository | standard_executable | framework | version_relationship | contract_version |"
	separator := "|---|---|---|---|---|---|---|---|---|"
	rowA := "| laravel | anvil-adapter-laravel | internal/laravel | anvil-standard-laravel | maleolabs/anvil-standard-laravel | anvil-adapter-laravel | Laravel | independent-lines | declared |"
	rowB := "| laravel | anvil-adapter-laravel2 | internal/laravel | anvil-standard-laravel2 | maleolabs/anvil-standard-laravel2 | anvil-adapter-laravel2 | Laravel | independent-lines | declared |"

	// Duplicate adapter_name.
	path := writeMappingFile(t, header+"\n"+separator+"\n"+rowA+"\n"+rowB+"\n")
	_, err := LoadAdapterMapping(path)
	if !errors.Is(err, ErrAdapterMappingInvalid) {
		t.Fatalf("error = %v, want wrapped ErrAdapterMappingInvalid", err)
	}
	if !strings.Contains(err.Error(), "adapter_name \"laravel\" appears in more than one row") {
		t.Errorf("error should name the duplicated adapter_name, got: %v", err)
	}

	// Duplicate adapter_executable.
	rowC := "| laravel2 | anvil-adapter-laravel | internal/laravel | anvil-standard-laravel2 | maleolabs/anvil-standard-laravel2 | anvil-adapter-laravel | Laravel | independent-lines | declared |"
	path = writeMappingFile(t, header+"\n"+separator+"\n"+rowA+"\n"+rowC+"\n")
	_, err = LoadAdapterMapping(path)
	if !errors.Is(err, ErrAdapterMappingInvalid) {
		t.Fatalf("error = %v, want wrapped ErrAdapterMappingInvalid", err)
	}
	if !strings.Contains(err.Error(), "adapter_executable \"anvil-adapter-laravel\" appears in more than one row") {
		t.Errorf("error should name the duplicated adapter_executable, got: %v", err)
	}
}

// TestLoadAdapterMapping_RowOrderIrrelevant verifies the §7 contract
// that row order is not part of the contract: two artifacts with the
// same rows in different order load to equivalent mappings.
func TestLoadAdapterMapping_RowOrderIrrelevant(t *testing.T) {
	header := "| adapter_name | adapter_executable | adapter_source | standard_id | standard_repository | standard_executable | framework | version_relationship | contract_version |"
	separator := "|---|---|---|---|---|---|---|---|---|"
	rowLaravel := "| laravel | anvil-adapter-laravel | internal/laravel; cmd/laravel-adapter | anvil-standard-laravel | maleolabs/anvil-standard-laravel | anvil-adapter-laravel | Laravel | independent-lines | declared by the standard's lifecycle-model contract |"
	rowFlutter := "| flutter | anvil-adapter-flutter | internal/flutter; cmd/flutter-adapter | anvil-standard-flutter | maleolabs/anvil-standard-flutter | anvil-adapter-flutter | Flutter | independent-lines | declared by the standard's lifecycle-model contract |"

	ordered := writeMappingFile(t, header+"\n"+separator+"\n"+rowLaravel+"\n"+rowFlutter+"\n")
	reversed := writeMappingFile(t, header+"\n"+separator+"\n"+rowFlutter+"\n"+rowLaravel+"\n")

	a, err := LoadAdapterMapping(ordered)
	if err != nil {
		t.Fatalf("load ordered mapping: %v", err)
	}
	b, err := LoadAdapterMapping(reversed)
	if err != nil {
		t.Fatalf("load reversed mapping: %v", err)
	}

	// Multi-value adapter_source splits on ';' (§7).
	laravel, ok := a.LookupByAdapterName("laravel")
	if !ok {
		t.Fatal("laravel row missing from ordered mapping")
	}
	if !reflect.DeepEqual(laravel.AdapterSource, []string{"internal/laravel", "cmd/laravel-adapter"}) {
		t.Errorf("adapter_source = %v, want [internal/laravel cmd/laravel-adapter]", laravel.AdapterSource)
	}

	for _, name := range []string{"laravel", "flutter"} {
		rowA, okA := a.LookupByAdapterName(name)
		rowB, okB := b.LookupByAdapterName(name)
		if !okA || !okB {
			t.Fatalf("row %q missing in one of the mappings (okA=%v okB=%v)", name, okA, okB)
		}
		if !reflect.DeepEqual(rowA, rowB) {
			t.Errorf("row %q differs by row order: %+v vs %+v", name, rowA, rowB)
		}
	}
}

// TestLoadAdapterMapping_SizeCap verifies that an oversize artifact
// fails load with an actionable error instead of unbounded memory use.
func TestLoadAdapterMapping_SizeCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mapping.md")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), MaxAdapterMappingSize+1), 0644); err != nil {
		t.Fatalf("write oversize mapping file: %v", err)
	}
	_, err := LoadAdapterMapping(path)
	if !strings.Contains(err.Error(), "size cap") {
		t.Fatalf("error = %v, want the size-cap failure", err)
	}
}

// TestLoadAdapterMapping_NoDataRows verifies that a table with the
// field-contract header but ZERO data rows is rejected (fail-closed):
// the table must carry one row per first-party v1.x adapter (§7) — an
// empty mapping can never supply the mapping and must not pass as one.
func TestLoadAdapterMapping_NoDataRows(t *testing.T) {
	header := "| adapter_name | adapter_executable | adapter_source | standard_id | standard_repository | standard_executable | framework | version_relationship | contract_version |"
	separator := "|---|---|---|---|---|---|---|---|---|"
	path := writeMappingFile(t, header+"\n"+separator+"\n\nSome text after the empty table.\n")
	_, err := LoadAdapterMapping(path)
	if !errors.Is(err, ErrAdapterMappingInvalid) {
		t.Fatalf("error = %v, want wrapped ErrAdapterMappingInvalid", err)
	}
	if !strings.Contains(err.Error(), "no data rows") {
		t.Errorf("error should name the missing data rows, got: %v", err)
	}
}

// TestLoadAdapterMapping_UnreadableFile verifies that a directory at the
// artifact path is an actionable error, not a silent skip.
func TestLoadAdapterMapping_UnreadableFile(t *testing.T) {
	_, err := LoadAdapterMapping(t.TempDir())
	if err == nil {
		t.Fatal("LoadAdapterMapping(directory) should fail")
	}
}

// TestResolveAdapterMappingPath verifies the path resolution order:
// explicit argument → ANVIL_ADAPTER_STANDARD_MAPPING → the documented
// default relative to the working directory.
func TestResolveAdapterMappingPath(t *testing.T) {
	t.Setenv(EnvAdapterStandardMapping, "/env/mapping.md")

	// 1. Explicit wins.
	got, err := ResolveAdapterMappingPath("/explicit/mapping.md", os.Getenv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/explicit/mapping.md" {
		t.Errorf("explicit path = %q, want /explicit/mapping.md", got)
	}

	// 2. Environment variable when no explicit path.
	got, err = ResolveAdapterMappingPath("", os.Getenv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/env/mapping.md" {
		t.Errorf("env path = %q, want /env/mapping.md", got)
	}

	// 3. Documented default when neither is set.
	got, err = ResolveAdapterMappingPath("", func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != DefaultAdapterMappingRelativePath {
		t.Errorf("default path = %q, want %q", got, DefaultAdapterMappingRelativePath)
	}
}

// writeMappingFile writes content to a fresh temp file and returns its
// path.
func writeMappingFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mapping.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write mapping file: %v", err)
	}
	return path
}
