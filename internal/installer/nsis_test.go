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

func intPtr(i int) *int { return &i }

func TestNSIS_GenerateFormsINI_6Types(t *testing.T) {
	forms := config.InstallerForms{
		"setup": {
			Title: "Setup Test",
			Fields: []config.FormField{
				{Name: "name", Type: "text", Required: true, Label: "Name"},
				{Name: "email", Type: "email", Required: true},
				{Name: "pwd", Type: "password", Required: true, MinLength: intPtr(8)},
				{Name: "role", Type: "select", Required: true, Options: []string{"admin", "user", "guest"}},
				{Name: "age", Type: "number", Required: false},
				{Name: "bio", Type: "textarea", Required: false},
			},
		},
	}
	ini, err := GenerateFormsINI(forms)
	if err != nil {
		t.Fatalf("GenerateFormsINI: %v", err)
	}
	// Must contain 6 Types entries reflecting mapping
	if strings.Count(ini, "Type=Text") < 3 { // text, email, number, textarea all Text
		t.Fatalf("expected at least 3 Text types, got ini:\n%s", ini)
	}
	if !strings.Contains(ini, "Type=Password") {
		t.Fatalf("expected Type=Password for password, ini:\n%s", ini)
	}
	if !strings.Contains(ini, "Type=Combobox") {
		t.Fatalf("expected Type=Combobox for select, ini:\n%s", ini)
	}
	if !strings.Contains(ini, "List=admin|user|guest") {
		t.Fatalf("expected List for select options, ini:\n%s", ini)
	}
	// textarea should have MULTILINE flag
	if !strings.Contains(ini, "Flags=MULTILINE") {
		t.Fatalf("expected Flags=MULTILINE for textarea, ini:\n%s", ini)
	}
	// Should have 12 fields (6 fields * label+input)
	if !strings.Contains(ini, "NumFields=12") {
		t.Fatalf("expected NumFields=12, ini:\n%s", ini)
	}
	// Each field name appears in comment
	for _, name := range []string{"name", "email", "pwd", "role", "age", "bio"} {
		if !strings.Contains(ini, "FieldName="+name) {
			t.Fatalf("expected FieldName=%s in ini", name)
		}
	}
}

func TestNSIS_Validation(t *testing.T) {
	forms := config.InstallerForms{
		"superAdmin": {
			Fields: []config.FormField{
				{Name: "username", Type: "text", Required: true, MinLength: intPtr(3)},
				{Name: "email", Type: "email", Required: true},
				{Name: "password", Type: "password", Required: true, MinLength: intPtr(8)},
				{Name: "confirm_password", Type: "password", Required: true, Confirmation: "password"},
				{Name: "age", Type: "number", Required: false},
				{Name: "role", Type: "select", Required: true, Options: []string{"admin", "user"}},
				{Name: "invite_code", Type: "text", Required: true, When: &config.WhenCondition{Field: "role", Value: "admin"}},
			},
		},
	}

	// 1. required failure
	values := map[string]map[string]string{
		"superAdmin": {"username": "", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "user"},
	}
	errs := ValidateFormValues(forms, values)
	if _, ok := errs["superAdmin.username"]; !ok {
		t.Fatalf("expected username required error, got %v", errs)
	}

	// 2. email invalid
	values = map[string]map[string]string{
		"superAdmin": {"username": "john", "email": "not-an-email", "password": "12345678", "confirm_password": "12345678", "role": "user"},
	}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["superAdmin.email"]; !ok {
		t.Fatalf("expected email invalid, got %v", errs)
	}

	// 3. minLength
	values = map[string]map[string]string{
		"superAdmin": {"username": "jo", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "user"},
	}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["superAdmin.username"]; !ok {
		t.Fatalf("expected minLength error, got %v", errs)
	}

	// 4. confirmation mismatch
	values = map[string]map[string]string{
		"superAdmin": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "different", "role": "user"},
	}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["superAdmin.confirm_password"]; !ok {
		t.Fatalf("expected confirmation mismatch, got %v", errs)
	}

	// 5. when conditional skip (invite_code required but when role=user should skip)
	values = map[string]map[string]string{
		"superAdmin": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "user"},
	}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["superAdmin.invite_code"]; ok {
		t.Fatalf("expected when conditional skip, should not error on invite_code when role=user, got %v", errs)
	}
	// when condition met -> should require
	values = map[string]map[string]string{
		"superAdmin": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "admin"},
	}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["superAdmin.invite_code"]; !ok {
		t.Fatalf("expected invite_code required when role=admin, got %v", errs)
	}

	// 6. valid case should have no errors
	values = map[string]map[string]string{
		"superAdmin": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "admin", "invite_code": "INVITE123", "age": "30"},
	}
	errs = ValidateFormValues(forms, values)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for valid values, got %v", errs)
	}

	// 7. number invalid
	values = map[string]map[string]string{
		"superAdmin": {"username": "john", "email": "a@b.com", "password": "12345678", "confirm_password": "12345678", "role": "user", "age": "not-a-number"},
	}
	errs = ValidateFormValues(forms, values)
	if _, ok := errs["superAdmin.age"]; !ok {
		t.Fatalf("expected number invalid, got %v", errs)
	}

	// 8. pattern check
	forms2 := config.InstallerForms{
		"f": {Fields: []config.FormField{{Name: "code", Type: "text", Pattern: "^[A-Z]{3}$"}}},
	}
	values2 := map[string]map[string]string{"f": {"code": "abc"}}
	errs = ValidateFormValues(forms2, values2)
	if _, ok := errs["f.code"]; !ok {
		t.Fatalf("expected pattern mismatch, got %v", errs)
	}
	values2 = map[string]map[string]string{"f": {"code": "ABC"}}
	errs = ValidateFormValues(forms2, values2)
	if len(errs) != 0 {
		t.Fatalf("expected pattern pass, got %v", errs)
	}
}

func TestNSIS_TitleFromForms(t *testing.T) {
	forms := config.InstallerForms{
		"setup": {Title: "My Setup Title", Fields: []config.FormField{{Name: "a", Type: "text"}}},
		"other": {Title: "Other", Fields: []config.FormField{{Name: "b", Type: "text"}}},
	}
	// sorted keys: other < setup, so first non-empty title is "Other"
	if got := InstallerTitle(forms); got != "Other" {
		t.Fatalf("title got %q want Other (sorted deterministic)", got)
	}
	formsEmpty := config.InstallerForms{
		"a": {Fields: []config.FormField{{Name: "x", Type: "text"}}},
	}
	if got := InstallerTitle(formsEmpty); got != DefaultInstallerTitle {
		t.Fatalf("expected fallback title %q got %q", DefaultInstallerTitle, got)
	}
	if got := InstallerTitle(nil); got != DefaultInstallerTitle {
		t.Fatalf("nil fallback failed got %q", got)
	}
}

func TestNSIS_BuildWindowsInstaller_GenericFlow(t *testing.T) {
	tmpSrc := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpSrc, "app.txt"), []byte("hello"), 0644)
	forms := config.InstallerForms{
		"superAdmin": {
			Title: "Super Admin Setup",
			Fields: []config.FormField{
				{Name: "email", Type: "email", Required: true},
				{Name: "password", Type: "password", Required: true, MinLength: intPtr(6)},
			},
		},
	}
	formsJSON, _ := config.MarshalFormsJSON(forms)
	artifactPath, err := func() (string, error) {
		res, err := artifact.BuildInstallerPayload(artifact.InstallerPayloadOptions{
			SourceDir: tmpSrc,
			OutputDir: t.TempDir(),
			Version:   "1.0.0",
			Source:    "src",
			ProjectID: "proj",
			FormsJSON: formsJSON,
		})
		if err != nil {
			return "", err
		}
		return res.BundlePath, nil
	}()
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	out := filepath.Join(t.TempDir(), "installer.nsi")
	if err := BuildWindowsInstaller(artifactPath, out); err != nil {
		t.Fatalf("BuildWindowsInstaller: %v", err)
	}
	// Check INI generated
	iniPath := strings.TrimSuffix(out, ".nsi") + ".ini"
	if _, err := os.Stat(iniPath); err != nil {
		t.Fatalf("ini not generated: %v", err)
	}
	iniData, _ := os.ReadFile(iniPath)
	if !strings.Contains(string(iniData), "Type=Text") {
		t.Fatalf("ini missing Text")
	}
	if !strings.Contains(string(iniData), "Type=Password") {
		t.Fatalf("ini missing Password")
	}
	scriptData, _ := os.ReadFile(out)
	script := string(scriptData)
	if !strings.Contains(script, "Super Admin Setup") {
		t.Fatalf("script should contain title, got:\n%s", script)
	}
	if !strings.Contains(script, "INSTALLER_FORMS_JSON") {
		t.Fatalf("script should set INSTALLER_FORMS_JSON env var")
	}
	if !strings.Contains(script, "MessageBox") {
		t.Fatalf("script should contain validation MessageBox")
	}
	// Check validation loop via Abort presence
	if !strings.Contains(script, "Abort") {
		t.Fatalf("script should contain Abort for validation loop")
	}
	// Ensure redacted: script should not contain value placeholder directly
	if strings.Contains(script, "password123") {
		t.Fatalf("script should not contain actual password")
	}
	_ = json.RawMessage{}
}

func TestNSIS_WriteFormsJSONTempFile_RedactedEnv(t *testing.T) {
	values := map[string]map[string]string{
		"superAdmin": {"email": "admin@example.com", "password": "s3cret"},
	}
	path, err := WriteFormsJSONTempFile(values)
	if err != nil {
		t.Fatalf("WriteFormsJSONTempFile: %v", err)
	}
	defer RemoveFormsJSONTempFile(path)
	if got := os.Getenv(FormsEnvVar); got != path {
		t.Fatalf("env var not set got %q want %q", got, path)
	}
	data, _ := os.ReadFile(path)
	var parsed map[string]map[string]map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal temp json: %v", err)
	}
	if parsed["forms"]["superAdmin"]["password"] != "s3cret" {
		t.Fatalf("temp json missing password")
	}
	// Redacted log should not contain secret when field name present
	line := "password: s3cret"
	redacted := RedactInstallerLogWithForms(line, []string{"password"})
	if strings.Contains(redacted, "s3cret") {
		t.Fatalf("redacted log should not contain secret, got %q", redacted)
	}
	// Also generic password literal
	line2 := "user password=s3cret and email admin@example.com"
	redacted2 := RedactInstallerLogWithForms(line2, []string{"password"})
	if strings.Contains(redacted2, "s3cret") {
		t.Fatalf("redacted2 should not contain secret, got %q", redacted2)
	}
}
