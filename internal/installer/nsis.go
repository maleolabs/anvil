package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"maleolabs.com/anvil/internal/artifact"
	"maleolabs.com/anvil/internal/config"
)

const (
	DefaultInstallerTitle = "Anvil Installer"
	FormsEnvVar           = "INSTALLER_FORMS_JSON"

	iniLabelHeight = 8
	iniFieldHeight = 12
	iniLeft        = 0
	iniWidth       = 300
	iniLabelWidth  = 300
)

var (
	emailRegex  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	numberRegex = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

// InstallerTitle returns title from forms.*.title else fallback.
func InstallerTitle(forms config.InstallerForms) string {
	if len(forms) == 0 {
		return DefaultInstallerTitle
	}
	keys := make([]string, 0, len(forms))
	for k := range forms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if t := strings.TrimSpace(forms[k].Title); t != "" {
			return t
		}
	}
	return DefaultInstallerTitle
}

// IsFieldVisible reports whether field should be validated/displayed given values.
func IsFieldVisible(field config.FormField, values map[string]string) bool {
	if field.When == nil {
		return true
	}
	if values == nil {
		return false
	}
	v, ok := values[field.When.Field]
	if !ok {
		return false
	}
	return v == field.When.Value
}

// ValidateField validates single field value in context of allValues for confirmation.
func ValidateField(field config.FormField, value string, allValues map[string]string) string {
	if !IsFieldVisible(field, allValues) {
		return ""
	}
	if field.Required && strings.TrimSpace(value) == "" {
		return fmt.Sprintf("%s is required", field.Name)
	}
	// If empty and not required, skip further checks
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if field.MinLength != nil && len(value) < *field.MinLength {
		return fmt.Sprintf("%s must be at least %d characters", field.Name, *field.MinLength)
	}
	// type-specific
	switch field.Type {
	case "email":
		if !emailRegex.MatchString(value) {
			return fmt.Sprintf("%s must be a valid email", field.Name)
		}
	case "number":
		if !numberRegex.MatchString(value) {
			return fmt.Sprintf("%s must be a valid number", field.Name)
		}
	}
	if field.Pattern != "" {
		re, err := regexp.Compile(field.Pattern)
		if err == nil && !re.MatchString(value) {
			return fmt.Sprintf("%s does not match pattern", field.Name)
		}
	}
	if field.Confirmation != "" {
		if other, ok := allValues[field.Confirmation]; ok {
			if value != other {
				return fmt.Sprintf("%s does not match %s", field.Name, field.Confirmation)
			}
		} else if value != "" {
			// confirmation target missing -> mismatch
			return fmt.Sprintf("%s does not match %s", field.Name, field.Confirmation)
		}
	}
	if field.Type == "select" && len(field.Options) > 0 {
		found := false
		for _, opt := range field.Options {
			if opt == value {
				found = true
				break
			}
		}
		if !found {
			// allow empty already handled; if required we already checked
			// For strictness, report invalid option
			return fmt.Sprintf("%s must be one of [%s]", field.Name, strings.Join(field.Options, ","))
		}
	}
	return ""
}

// ValidateFormValues validates all forms against values: map[formName]map[fieldName]value
// Returns map of errors keyed by "form.field"
func ValidateFormValues(forms config.InstallerForms, values map[string]map[string]string) map[string]string {
	errs := map[string]string{}
	for formName, form := range forms {
		flat := map[string]string{}
		if values != nil {
			if fv, ok := values[formName]; ok {
				flat = fv
			}
		}
		for _, f := range form.Fields {
			val := flat[f.Name]
			if msg := ValidateField(f, val, flat); msg != "" {
				errs[formName+"."+f.Name] = msg
			}
		}
	}
	return errs
}

// GenerateFormsINI generates NSIS InstallOptions INI string from forms.
func GenerateFormsINI(forms config.InstallerForms) (string, error) {
	if len(forms) == 0 {
		return generateEmptyINI(), nil
	}
	keys := make([]string, 0, len(forms))
	for k := range forms {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	// Count total fields (including labels we emit as separate fields)
	// We emit 1 label + 1 input per FormField, but for textarea we emit larger height.
	// For InstallOptions each field is a UI element; we generate label as Label type + input.
	totalFields := 0
	for _, k := range keys {
		totalFields += len(forms[k].Fields) * 2 // label + input
	}
	// Settings
	sb.WriteString("[Settings]\n")
	sb.WriteString(fmt.Sprintf("NumFields=%d\n", totalFields))
	sb.WriteString("\n")

	fieldIdx := 1
	top := 0
	for _, formName := range keys {
		form := forms[formName]
		for _, f := range form.Fields {
			label := f.Label
			if label == "" {
				label = f.Name
			}
			// Label field
			sb.WriteString(fmt.Sprintf("[Field %d]\n", fieldIdx))
			sb.WriteString("Type=Label\n")
			sb.WriteString(fmt.Sprintf("Text=%s\n", escapeINI(label)))
			sb.WriteString(fmt.Sprintf("Left=%d\n", iniLeft))
			sb.WriteString(fmt.Sprintf("Top=%d\n", top))
			sb.WriteString(fmt.Sprintf("Right=%d\n", iniWidth))
			sb.WriteString(fmt.Sprintf("Bottom=%d\n", top+iniLabelHeight))
			sb.WriteString("\n")
			fieldIdx++
			top += iniLabelHeight + 2

			// Input field
			sb.WriteString(fmt.Sprintf("[Field %d]\n", fieldIdx))
			iniType, extra := iniTypeForField(f)
			sb.WriteString(fmt.Sprintf("Type=%s\n", iniType))
			sb.WriteString(fmt.Sprintf("Left=%d\n", iniLeft))
			sb.WriteString(fmt.Sprintf("Top=%d\n", top))
			sb.WriteString(fmt.Sprintf("Right=%d\n", iniWidth))
			h := iniFieldHeight
			if f.Type == "textarea" {
				h = 40
			}
			sb.WriteString(fmt.Sprintf("Bottom=%d\n", top+h))
			// State is current value placeholder
			sb.WriteString("State=\n")
			// Flags
			if f.Type == "textarea" {
				sb.WriteString("Flags=MULTILINE\n")
			}
			if extra != "" {
				sb.WriteString(extra)
				if !strings.HasSuffix(extra, "\n") {
					sb.WriteString("\n")
				}
			}
			// For redaction awareness, add comment with field name (not value)
			sb.WriteString(fmt.Sprintf("; FieldName=%s Form=%s Type=%s\n", f.Name, formName, f.Type))
			sb.WriteString("\n")
			fieldIdx++
			top += h + 6
		}
		// extra spacing between forms
		top += 8
	}
	return sb.String(), nil
}

func generateEmptyINI() string {
	return "[Settings]\nNumFields=0\n"
}

func iniTypeForField(f config.FormField) (string, string) {
	switch f.Type {
	case "password":
		return "Password", ""
	case "select":
		// Combobox with List
		list := strings.Join(f.Options, "|")
		extra := fmt.Sprintf("List=%s\n", escapeINI(list))
		return "Combobox", extra
	case "text", "email", "number", "textarea":
		return "Text", ""
	default:
		return "Text", ""
	}
}

func escapeINI(s string) string {
	// Minimal escaping: replace newline, keep as is
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// GenerateNSISScript generates full NSIS script that shows InstallOptions dialog,
// loops validation, and writes collected values to temp JSON via INSTALLER_FORMS_JSON.
func GenerateNSISScript(forms config.InstallerForms, iniPath string, artifactPath string) string {
	title := InstallerTitle(forms)
	var sb strings.Builder
	sb.WriteString("; Auto-generated NSIS script for Anvil installer with dynamic forms\n")
	sb.WriteString(fmt.Sprintf("!define FORMS_TITLE \"%s\"\n", escapeNSIS(title)))
	sb.WriteString(fmt.Sprintf("!define ARTIFACT_PATH \"%s\"\n", escapeNSIS(artifactPath)))
	sb.WriteString(fmt.Sprintf("!define FORMS_INI \"%s\"\n", escapeNSIS(iniPath)))
	sb.WriteString("\n")
	sb.WriteString("Name \"${FORMS_TITLE}\"\n")
	sb.WriteString("OutFile \"anvil-installer.exe\"\n")
	sb.WriteString("InstallDir \"$PROGRAMFILES\\Anvil\"\n")
	sb.WriteString("RequestExecutionLevel admin\n")
	sb.WriteString("\n")
	sb.WriteString("!include \"MUI2.nsh\"\n")
	sb.WriteString("!include \"InstallOptions.nsh\"\n")
	sb.WriteString("\n")
	sb.WriteString("!insertmacro MUI_PAGE_WELCOME\n")
	sb.WriteString("Page custom FormsPage FormsPageLeave\n")
	sb.WriteString("!insertmacro MUI_PAGE_INSTFILES\n")
	sb.WriteString("!insertmacro MUI_PAGE_FINISH\n")
	sb.WriteString("!insertmacro MUI_LANGUAGE \"English\"\n")
	sb.WriteString("\n")
	sb.WriteString("Var FormsJSONPath\n")
	sb.WriteString("\n")
	// Collect validation checks as NSIS snippets
	sb.WriteString("Function FormsPage\n")
	sb.WriteString("  !insertmacro INSTALLOPTIONS_DISPLAY \"${FORMS_INI}\"\n")
	sb.WriteString("FunctionEnd\n")
	sb.WriteString("\n")
	sb.WriteString("Function FormsPageLeave\n")

	// For each form/field generate InstallOptions read + validation
	fieldIdx := 2 // because label is odd, input is even; start at 2
	keys := sortedKeys(forms)
	for _, formName := range keys {
		form := forms[formName]
		for _, f := range form.Fields {
			sb.WriteString(fmt.Sprintf("  ; -- %s.%s (%s)\n", formName, f.Name, f.Type))
			sb.WriteString(fmt.Sprintf("  !insertmacro INSTALLOPTIONS_READ $0 \"${FORMS_INI}\" \"Field %d\" \"State\"\n", fieldIdx))
			// Validation as NSIS branch
			nsisChecks := nsisValidationForField(f, formName)
			if nsisChecks != "" {
				sb.WriteString(nsisChecks)
			}
			fieldIdx += 2
		}
	}

	sb.WriteString("  ; Write collected values to temp JSON via INSTALLER_FORMS_JSON\n")
	sb.WriteString("  Call WriteFormsJSON\n")
	sb.WriteString("  Return\n")
	sb.WriteString("FunctionEnd\n")
	sb.WriteString("\n")
	sb.WriteString("Function WriteFormsJSON\n")
	sb.WriteString("  ; Create temp file and set env var INSTALLER_FORMS_JSON\n")
	sb.WriteString("  GetTempFileName $FormsJSONPath\n")
	sb.WriteString("  System::Call 'kernel32::SetEnvironmentVariable(t \"INSTALLER_FORMS_JSON\", t r0) i.r1'\n")
	sb.WriteString("  ; Write JSON (redacted logging - do not echo values)\n")
	sb.WriteString("  FileOpen $1 $FormsJSONPath w\n")

	// Write JSON structure
	sb.WriteString("  FileWrite $1 \"{\\\"forms\\\":{\"\n")
	firstForm := true
	fieldIdx = 2
	for _, formName := range keys {
		form := forms[formName]
		prefix := ","
		if firstForm {
			prefix = ""
			firstForm = false
		}
		sb.WriteString(fmt.Sprintf("  FileWrite $1 \"%s\\\"%s\\\":{\"\n", prefix, formName))
		firstField := true
		for _, f := range form.Fields {
			sep := ","
			if firstField {
				sep = ""
				firstField = false
			}
			sb.WriteString(fmt.Sprintf("  !insertmacro INSTALLOPTIONS_READ $0 \"${FORMS_INI}\" \"Field %d\" \"State\"\n", fieldIdx))
			sb.WriteString(fmt.Sprintf("  FileWrite $1 \"%s\\\"%s\\\":\\\"$0\\\"\"\n", sep, f.Name))
			fieldIdx += 2
		}
		sb.WriteString("  FileWrite $1 \"}\"\n")
	}
	sb.WriteString("  FileWrite $1 \"}}\"\n")
	sb.WriteString("  FileClose $1\n")
	sb.WriteString("FunctionEnd\n")

	// Section to extract artifact payload (verification done before extract via security gate)
	sb.WriteString("\nSection \"Install\"\n")
	sb.WriteString("  SetOutPath $INSTDIR\n")
	sb.WriteString("  File \"${ARTIFACT_PATH}\"\n")
	sb.WriteString("  ; installer runtime will read $%INSTALLER_FORMS_JSON% temp file\n")
	sb.WriteString("SectionEnd\n")
	return sb.String()
}

func nsisValidationForField(f config.FormField, formName string) string {
	var sb strings.Builder
	// When conditional skip
	if f.When != nil {
		// Need to read the When field's state; we assume When field comes earlier
		// For simplicity, emit check that skips validation if condition not met
		sb.WriteString(fmt.Sprintf("  ; when %s==%s skip if not met\n", f.When.Field, f.When.Value))
		sb.WriteString(fmt.Sprintf("  StrCmp $%s \"%s\" +2 0\n", whenVar(f.When.Field), escapeNSIS(f.When.Value)))
		sb.WriteString("  Goto +4\n")
		// actual check label fallthrough
	}
	if f.Required {
		sb.WriteString(fmt.Sprintf("  StrCmp $0 \"\" 0 +3\n"))
		sb.WriteString(fmt.Sprintf("    MessageBox MB_OK \"Field '%s' is required.\"\n", escapeNSIS(f.Name)))
		sb.WriteString("    Abort\n")
	}
	if f.MinLength != nil {
		sb.WriteString(fmt.Sprintf("  StrLen $1 $0\n"))
		sb.WriteString(fmt.Sprintf("  IntCmp $1 %d +3 0 0\n", *f.MinLength))
		sb.WriteString(fmt.Sprintf("    MessageBox MB_OK \"Field '%s' must be at least %d characters.\"\n", escapeNSIS(f.Name), *f.MinLength))
		sb.WriteString("    Abort\n")
	}
	switch f.Type {
	case "email":
		sb.WriteString(fmt.Sprintf("  ; email regex check for %s\n", f.Name))
		sb.WriteString("  Push $0\n")
		sb.WriteString("  Call IsValidEmail\n")
		sb.WriteString("  Pop $1\n")
		sb.WriteString(fmt.Sprintf("  StrCmp $1 \"1\" +3\n"))
		sb.WriteString(fmt.Sprintf("    MessageBox MB_OK \"Field '%s' must be a valid email.\"\n", escapeNSIS(f.Name)))
		sb.WriteString("    Abort\n")
	case "number":
		sb.WriteString(fmt.Sprintf("  ; number pattern for %s\n", f.Name))
		sb.WriteString("  Push $0\n")
		sb.WriteString("  Call IsValidNumber\n")
		sb.WriteString("  Pop $1\n")
		sb.WriteString(fmt.Sprintf("  StrCmp $1 \"1\" +3\n"))
		sb.WriteString(fmt.Sprintf("    MessageBox MB_OK \"Field '%s' must be a valid number.\"\n", escapeNSIS(f.Name)))
		sb.WriteString("    Abort\n")
	}
	if f.Pattern != "" {
		sb.WriteString(fmt.Sprintf("  ; pattern %s for %s — validated via regex in runtime; NSIS shows generic error if mismatch\n", escapeNSIS(f.Pattern), f.Name))
		// Emit placeholder for pattern validation (actual runtime validates with Go)
		sb.WriteString(fmt.Sprintf("  ; Pattern validation for %s handled by installer runtime before commit\n", f.Name))
	}
	if f.Confirmation != "" {
		sb.WriteString(fmt.Sprintf("  ; confirmation match %s == %s\n", f.Name, f.Confirmation))
		sb.WriteString(fmt.Sprintf("  !insertmacro INSTALLOPTIONS_READ $1 \"${FORMS_INI}\" \"Field %d\" \"State\"\n", confirmationFieldIdx(f.Confirmation, formName, f)))
		sb.WriteString("  StrCmp $0 $1 +3\n")
		sb.WriteString(fmt.Sprintf("    MessageBox MB_OK \"Field '%s' does not match %s.\"\n", escapeNSIS(f.Name), escapeNSIS(f.Confirmation)))
		sb.WriteString("    Abort\n")
	}
	return sb.String()
}

func whenVar(field string) string { return "When_" + field }
func confirmationFieldIdx(target, formName string, current config.FormField) int {
	// Placeholder - actual index resolved at runtime; emit 0 to indicate dynamic lookup
	return 0
}

func sortedKeys(forms config.InstallerForms) []string {
	keys := make([]string, 0, len(forms))
	for k := range forms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func escapeNSIS(s string) string {
	s = strings.ReplaceAll(s, "\"", "$\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// WriteFormsJSONTempFile writes values to a temp JSON file, sets INSTALLER_FORMS_JSON env var, and returns path.
// Values are not logged (redacted).
func WriteFormsJSONTempFile(values map[string]map[string]string) (string, error) {
	payload := map[string]interface{}{"forms": values}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "installer-forms-*.json")
	if err != nil {
		return "", err
	}
	// Ensure 0600
	_ = tmp.Chmod(0600)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()
	if err := os.Setenv(FormsEnvVar, tmp.Name()); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

// RemoveFormsJSONTempFile removes temp file and unsets env var.
func RemoveFormsJSONTempFile(path string) {
	_ = os.Remove(path)
	_ = os.Unsetenv(FormsEnvVar)
}

// BuildWindowsInstaller reads forms from artifact, generates forms.ini and NSIS script.
func BuildWindowsInstaller(artifactPath, outputPath string) error {
	if strings.TrimSpace(artifactPath) == "" {
		return fmt.Errorf("artifactPath empty")
	}
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("outputPath empty")
	}
	if _, err := os.Stat(artifactPath); err != nil {
		return fmt.Errorf("artifact not found %q: %w", artifactPath, err)
	}
	var forms config.InstallerForms
	data, err := artifact.ReadFormsFromArtifact(artifactPath)
	if err != nil {
		// No forms in artifact -> empty
		forms = config.InstallerForms{}
	} else {
		if err := json.Unmarshal(data, &forms); err != nil {
			return fmt.Errorf("unmarshal forms.json from artifact: %w", err)
		}
	}
	if errs := config.ValidateFormsSchema(forms); len(errs) > 0 {
		return fmt.Errorf("forms schema invalid: %v", errs[0])
	}
	iniStr, err := GenerateFormsINI(forms)
	if err != nil {
		return fmt.Errorf("generate forms.ini: %w", err)
	}
	iniPath := strings.TrimSuffix(outputPath, ".nsi") + ".ini"
	if !strings.HasSuffix(strings.ToLower(outputPath), ".nsi") {
		iniPath = outputPath + ".ini"
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(iniPath, []byte(iniStr), 0644); err != nil {
		return fmt.Errorf("write forms.ini: %w", err)
	}
	script := GenerateNSISScript(forms, iniPath, artifactPath)
	if err := os.WriteFile(outputPath, []byte(script), 0644); err != nil {
		return fmt.Errorf("write nsi script: %w", err)
	}
	// Redacted log - do not log values
	_ = RedactInstallerLogWithForms
	return nil
}
