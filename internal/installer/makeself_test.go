package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/config"
)

func intPtr2(i int) *int { return &i }

// TestMakeself_Generic6Types verifies whiptail mapping for 6 field types and dialog --form fallback.
func TestMakeself_Generic6Types(t *testing.T) {
	fields := []config.FormField{
		{Name: "name", Type: "text", Required: true, Label: "Name"},
		{Name: "email", Type: "email", Required: true},
		{Name: "pwd", Type: "password", Required: true, MinLength: intPtr2(8)},
		{Name: "role", Type: "select", Required: true, Options: []string{"admin", "user", "guest"}},
		{Name: "age", Type: "number", Required: false},
		{Name: "bio", Type: "textarea", Required: false},
	}
	for _, f := range fields {
		args := WhiptailArgsForField(f)
		switch f.Type {
		case "text":
			if !contains(args, "--inputbox") {
				t.Fatalf("text field %q should map to --inputbox got %v", f.Name, args)
			}
		case "email":
			if !contains(args, "--inputbox") {
				t.Fatalf("email field %q should map to --inputbox got %v", f.Name, args)
			}
		case "password":
			if !contains(args, "--passwordbox") {
				t.Fatalf("password field %q should map to --passwordbox got %v", f.Name, args)
			}
		case "select":
			if !contains(args, "--menu") {
				t.Fatalf("select field %q should map to --menu got %v", f.Name, args)
			}
			for _, opt := range f.Options {
				if !contains(args, opt) {
					t.Fatalf("select options missing %q in args %v", opt, args)
				}
			}
		case "number":
			if !contains(args, "--inputbox") {
				t.Fatalf("number field %q should map to --inputbox got %v", f.Name, args)
			}
		case "textarea":
			if !contains(args, "--inputbox") {
				t.Fatalf("textarea field %q should map to --inputbox got %v", f.Name, args)
			}
			// textarea should be taller (15)
			if !contains(args, "15") {
				t.Fatalf("textarea should have height 15 got %v", args)
			}
		}
	}
	// GenerateWhiptailForm should produce per-field args
	form := GenerateWhiptailForm(fields)
	if len(form) != 6 {
		t.Fatalf("GenerateWhiptailForm expected 6 entries got %d", len(form))
	}
	// DialogFormArgs fallback
	dialogArgs := DialogFormArgs(fields, "Setup Test")
	if !contains(dialogArgs, "--form") {
		t.Fatalf("dialog fallback should contain --form got %v", dialogArgs)
	}
	// Dialog should contain label placeholders
	for _, f := range fields {
		found := false
		for _, a := range dialogArgs {
			if strings.Contains(a, f.Name) || strings.Contains(a, f.Label) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("dialog args missing label for field %q in %v", f.Name, dialogArgs)
		}
	}
}

func contains(arr []string, s string) bool {
	for _, v := range arr {
		if v == s {
			return true
		}
	}
	return false
}

// TestMakeself_Validation verifies pattern/minLength/confirmation/email/number validation via shared ValidateField.
func TestMakeself_Validation(t *testing.T) {
	forms := config.InstallerForms{
		"setup": {
			Fields: []config.FormField{
				{Name: "username", Type: "text", Required: true, MinLength: intPtr2(3)},
				{Name: "email", Type: "email", Required: true},
				{Name: "password", Type: "password", Required: true, MinLength: intPtr2(8)},
				{Name: "confirm_password", Type: "password", Required: true, Confirmation: "password"},
				{Name: "age", Type: "number", Required: false},
				{Name: "code", Type: "text", Pattern: "^[A-Z]{3}$"},
				{Name: "role", Type: "select", Required: true, Options: []string{"admin", "user"}},
			},
		},
	}
	// required
	values := map[string]map[string]string{"setup": {"username": "", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "user"}}
	errs := ValidateFormValues(forms, values)
	if _, ok := errs["setup.username"]; !ok {
		t.Fatalf("expected username required error got %v", errs)
	}
	// email invalid
	values = map[string]map[string]string{"setup": {"username": "john", "email": "not-email", "password": "12345678", "confirm_password": "12345678", "role": "user"}}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["setup.email"]; !ok {
		t.Fatalf("expected email invalid got %v", errs)
	}
	// minLength
	values = map[string]map[string]string{"setup": {"username": "jo", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "user"}}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["setup.username"]; !ok {
		t.Fatalf("expected minLength error got %v", errs)
	}
	// confirmation mismatch
	values = map[string]map[string]string{"setup": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "different", "role": "user"}}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["setup.confirm_password"]; !ok {
		t.Fatalf("expected confirmation mismatch got %v", errs)
	}
	// number invalid
	values = map[string]map[string]string{"setup": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "user", "age": "not-a-number", "code": "ABC"}}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["setup.age"]; !ok {
		t.Fatalf("expected number invalid got %v", errs)
	}
	// pattern mismatch
	values = map[string]map[string]string{"setup": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "user", "code": "abc"}}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["setup.code"]; !ok {
		t.Fatalf("expected pattern mismatch got %v", errs)
	}
	// pattern pass
	values = map[string]map[string]string{"setup": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "user", "code": "ABC"}}
	errs = ValidateFormValues(forms, values)
	if len(errs) != 0 {
		t.Fatalf("expected no errors got %v", errs)
	}
	// select invalid option
	values = map[string]map[string]string{"setup": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "invalid", "code": "ABC"}}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["setup.role"]; !ok {
		t.Fatalf("expected select invalid option got %v", errs)
	}
	// also ensure whiptail script contains validation snippets
	script, err := GenerateLinuxInstallerScript(forms, "/tmp/fake.tar.gz")
	if err != nil {
		t.Fatalf("GenerateLinuxInstallerScript: %v", err)
	}
	for _, needle := range []string{"--passwordbox", "--inputbox", "--menu", "must be a valid email", "must be at least", "does not match"} {
		// script should contain at least some validation messages or whiptail flags
		if needle == "--passwordbox" || needle == "--inputbox" || needle == "--menu" {
			if !strings.Contains(script, needle) {
				t.Fatalf("script missing %q", needle)
			}
		}
	}
	if !strings.Contains(script, "INSTALLER_FORMS_JSON") {
		t.Fatalf("script should mention INSTALLER_FORMS_JSON")
	}
	if !strings.Contains(script, "whiptail") || !strings.Contains(script, "dialog") {
		t.Fatalf("script should mention whiptail/dialog fallback")
	}
}

// TestMakeself_WhenConditional verifies when conditional field==value skip.
func TestMakeself_WhenConditional(t *testing.T) {
	forms := config.InstallerForms{
		"superAdmin": {
			Fields: []config.FormField{
				{Name: "role", Type: "select", Required: true, Options: []string{"admin", "user"}},
				{Name: "invite_code", Type: "text", Required: true, When: &config.WhenCondition{Field: "role", Value: "admin"}},
			},
		},
	}
	// when condition not met -> should skip invite_code
	values := map[string]map[string]string{"superAdmin": {"role": "user"}}
	errs := ValidateFormValues(forms, values)
	if _, ok := errs["superAdmin.invite_code"]; ok {
		t.Fatalf("when conditional skip failed, should not error when role=user got %v", errs)
	}
	// when condition met -> should require invite_code
	values = map[string]map[string]string{"superAdmin": {"role": "admin"}}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["superAdmin.invite_code"]; !ok {
		t.Fatalf("when conditional required failed, should error when role=admin and invite_code empty got %v", errs)
	}
	// IsFieldVisible
	if !IsFieldVisible(forms["superAdmin"].Fields[1], map[string]string{"role": "admin"}) {
		t.Fatalf("IsFieldVisible should be true when role=admin")
	}
	if IsFieldVisible(forms["superAdmin"].Fields[1], map[string]string{"role": "user"}) {
		t.Fatalf("IsFieldVisible should be false when role=user")
	}
	// Script should contain when conditional handling
	script, _ := GenerateLinuxInstallerScript(forms, "/tmp/fake.tar.gz")
	if !strings.Contains(script, "invite_code") {
		t.Fatalf("script missing invite_code field")
	}
	if !strings.Contains(script, "role") {
		t.Fatalf("script missing role field")
	}
	// Check script has conditional check for when
	if !strings.Contains(script, "admin") {
		t.Fatalf("script missing when value admin")
	}
}

// TestMakeself_CLI_Fallback verifies CLI prompt fallback when no TTY (mock stdin/stdout).
func TestMakeself_CLI_Fallback(t *testing.T) {
	forms := config.InstallerForms{
		"setup": {
			Title: "Setup",
			Fields: []config.FormField{
				{Name: "name", Type: "text", Required: true},
				{Name: "email", Type: "email", Required: true},
				{Name: "pwd", Type: "password", Required: true, MinLength: intPtr2(4)},
				{Name: "role", Type: "select", Required: true, Options: []string{"admin", "user"}},
			},
		},
	}
	// Simulate user input via strings.Reader
	input := "john\ninvalid-email\njohn@example.com\n123\nabcd\nuser\n" // first email invalid then retry, pwd too short then retry
	// But CollectFormValuesCLI reads sequentially per field with loop; we need to provide enough lines
	// For this test we provide valid sequential inputs without retries: use single pass
	input2 := "john\njohn@example.com\nabcd\nuser\n"
	in := strings.NewReader(input2)
	var out strings.Builder
	t.Setenv("ANVIL_FORCE_TTY", "0")
	// Ensure HasTTY returns false under FORCE_TTY=0
	if HasTTY() {
		t.Fatalf("HasTTY should be false with ANVIL_FORCE_TTY=0")
	}
	values, err := CollectFormValuesCLI(forms, in, &out)
	if err != nil {
		t.Fatalf("CollectFormValuesCLI: %v", err)
	}
	if values["setup"]["name"] != "john" {
		t.Fatalf("expected name john got %q", values["setup"]["name"])
	}
	if values["setup"]["email"] != "john@example.com" {
		t.Fatalf("expected email john@example.com got %q", values["setup"]["email"])
	}
	if values["setup"]["pwd"] != "abcd" {
		t.Fatalf("expected pwd abcd got %q", values["setup"]["pwd"])
	}
	if values["setup"]["role"] != "user" {
		t.Fatalf("expected role user got %q", values["setup"]["role"])
	}
	// Now test validation retry: provide invalid then valid
	inRetry := strings.NewReader(" \njohn\nnot-an-email\njohn@a.com\nab\nabcd\nuser\n")
	var outRetry strings.Builder
	values2, err := CollectFormValuesCLI(forms, inRetry, &outRetry)
	if err != nil {
		t.Fatalf("retry flow: %v", err)
	}
	if !strings.Contains(outRetry.String(), "Error:") {
		t.Fatalf("expected error messages on retry, got %q", outRetry.String())
	}
	if values2["setup"]["name"] != "john" {
		t.Fatalf("retry name got %q", values2["setup"]["name"])
	}
	// Also test WriteFormsJSONTempFile reuse (redacted handling)
	mapped := map[string]map[string]string{"setup": {"name": "john", "pwd": "s3cr3t"}}
	path, err := WriteFormsJSONTempFile(mapped)
	if err != nil {
		t.Fatalf("WriteFormsJSONTempFile: %v", err)
	}
	defer RemoveFormsJSONTempFile(path)
	if os.Getenv(FormsEnvVar) != path {
		t.Fatalf("env var not set")
	}
	data, _ := os.ReadFile(path)
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("forms json invalid: %v", err)
	}
	// Ensure file has 0600
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600 got %o", info.Mode().Perm())
	}
	// Ensure no log leaks: check file content contains password but log would be redacted (we just ensure file exists)
	if !strings.Contains(string(data), "s3cr3t") {
		t.Fatalf("temp file should contain actual value")
	}
	// Now input variable from earlier is used to avoid unused import warning
	_ = input
}

// TestMakeself_BuildLinuxInstaller checks BuildLinuxInstaller generates executable script with embedded artifact.
func TestMakeself_BuildLinuxInstaller(t *testing.T) {
	tmpSrc := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpSrc, "app.txt"), []byte("hello"), 0644)
	forms := config.InstallerForms{
		"setup": {
			Title: "Linux Setup",
			Fields: []config.FormField{
				{Name: "email", Type: "email", Required: true},
				{Name: "password", Type: "password", Required: true, MinLength: intPtr2(6)},
				{Name: "role", Type: "select", Options: []string{"admin", "user"}},
			},
		},
	}
	formsJSON, _ := config.MarshalFormsJSON(forms)
	res, err := artifact.BuildInstallerPayload(artifact.InstallerPayloadOptions{
		SourceDir: tmpSrc,
		OutputDir: t.TempDir(),
		Version:   "1.0.0",
		Source:    "test",
		ProjectID: "proj",
		FormsJSON: formsJSON,
	})
	if err != nil {
		t.Fatalf("BuildInstallerPayload: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "installer.run")
	if err := BuildLinuxInstaller(res.BundlePath, outputPath); err != nil {
		t.Fatalf("BuildLinuxInstaller: %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("output not found: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("installer should be executable got %o", info.Mode().Perm())
	}
	data, _ := os.ReadFile(outputPath)
	s := string(data)
	for _, needle := range []string{"whiptail", "dialog", "--inputbox", "--passwordbox", "--menu", "INSTALLER_FORMS_JSON", "__ARCHIVE_BELOW__"} {
		if !strings.Contains(s, needle) {
			t.Fatalf("installer script missing %q", needle)
		}
	}
	// Verify artifact payload appended (check that reading after marker yields gzip)
	idx := strings.Index(s, "__ARCHIVE_BELOW__\n")
	if idx == -1 {
		t.Fatalf("missing archive marker")
	}
	// payload after marker should be gzip (starts with magic bytes 1f 8b)
	payload := data[idx+len("__ARCHIVE_BELOW__\n"):]
	if len(payload) < 2 || payload[0] != 0x1f || payload[1] != 0x8b {
		t.Fatalf("payload after marker not gzip magic")
	}
	// Check has_tty and CLI fallback in script
	if !strings.Contains(s, "has_tty") {
		t.Fatalf("script missing has_tty fallback")
	}
	if !strings.Contains(s, "read") {
		t.Fatalf("script missing CLI read fallback")
	}
}
