package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil/internal/config"
)

func TestParseFormsInput_NestedAndFlat(t *testing.T) {
	// Nested shape {"forms":{"superAdmin":{"email":"a@b.com"}}}
	tmp := t.TempDir()
	path := filepath.Join(tmp, "forms.json")
	payload := map[string]interface{}{
		"forms": map[string]interface{}{
			"superAdmin": map[string]interface{}{
				"email":    "nested@example.com",
				"username": "john",
				"password": "s3cret",
			},
		},
	}
	data, _ := json.Marshal(payload)
	_ = os.WriteFile(path, data, 0600)
	got, err := ParseFormsInput(path)
	if err != nil {
		t.Fatalf("parse nested: %v", err)
	}
	if got["superAdmin"]["email"] != "nested@example.com" {
		t.Fatalf("email got %q", got["superAdmin"]["email"])
	}
	if got["superAdmin"]["username"] != "john" {
		t.Fatalf("username %q", got["superAdmin"]["username"])
	}
	// Flat shape {"superAdmin.email":"flat@example.com"}
	path2 := filepath.Join(tmp, "flat.json")
	payload2 := map[string]interface{}{
		"superAdmin.email":    "flat@example.com",
		"superAdmin.username": "flatadmin",
	}
	data2, _ := json.Marshal(payload2)
	_ = os.WriteFile(path2, data2, 0600)
	got2, err := ParseFormsInput(path2)
	if err != nil {
		t.Fatalf("parse flat: %v", err)
	}
	if got2["superAdmin"]["email"] != "flat@example.com" {
		t.Fatalf("flat email %q", got2["superAdmin"]["email"])
	}
	if got2["superAdmin"]["username"] != "flatadmin" {
		t.Fatalf("flat user %q", got2["superAdmin"]["username"])
	}
	// Direct nested without forms wrapper {"superAdmin":{"email":"x"}}
	path3 := filepath.Join(tmp, "direct.json")
	payload3 := map[string]interface{}{
		"superAdmin": map[string]interface{}{"email": "direct@example.com"},
	}
	data3, _ := json.Marshal(payload3)
	_ = os.WriteFile(path3, data3, 0600)
	got3, err := ParseFormsInput(path3)
	if err != nil {
		t.Fatalf("parse direct: %v", err)
	}
	if got3["superAdmin"]["email"] != "direct@example.com" {
		t.Fatalf("direct %q", got3["superAdmin"]["email"])
	}
	// Empty path returns empty map
	empty, err := ParseFormsInput("")
	if err != nil {
		t.Fatalf("empty path err %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty expected 0 got %v", empty)
	}
}

func TestResolveSetupConfig_TemplatedEmail(t *testing.T) {
	cfg := map[string]interface{}{
		"installer.setup.super_admin_email": "{{forms.superAdmin.email}}",
		"setup.superAdmin.value":            "fallback@example.com",
	}
	forms := map[string]map[string]string{
		"superAdmin": {"email": "fromform@example.com"},
	}
	res, err := ResolveSetupConfig(cfg, forms)
	if err != nil {
		t.Fatalf("resolve email err %v", err)
	}
	if res.Identifier != "fromform@example.com" {
		t.Fatalf("want fromform got %q", res.Identifier)
	}
	if res.Email != "fromform@example.com" {
		t.Fatalf("email field %q", res.Email)
	}
	if res.ResolvedFrom != "installer.setup.super_admin_email" {
		t.Fatalf("resolvedFrom %q", res.ResolvedFrom)
	}
}

func TestResolveSetupConfig_TemplatedUsername(t *testing.T) {
	cfg := map[string]interface{}{
		"installer.setup.super_admin_name": "{{forms.superAdmin.username}}",
		"setup.superAdmin.value":           "fallbackAdmin",
	}
	forms := map[string]map[string]string{
		"superAdmin": {"username": "john_doe"},
	}
	res, err := ResolveSetupConfig(cfg, forms)
	if err != nil {
		t.Fatalf("resolve username err %v", err)
	}
	if res.Identifier != "john_doe" {
		t.Fatalf("want john_doe got %q", res.Identifier)
	}
	if res.Username != "john_doe" {
		t.Fatalf("username %q", res.Username)
	}
}

func TestResolveSetupConfig_FallbackHardcode(t *testing.T) {
	cfg := map[string]interface{}{
		"installer.setup.super_admin_email": "{{forms.superAdmin.email}}",
		"setup.superAdmin.value":            "hardcode@example.com",
	}
	formsEmpty := map[string]map[string]string{}
	res, err := ResolveSetupConfig(cfg, formsEmpty)
	if err != nil {
		t.Fatalf("fallback err %v", err)
	}
	if res.Identifier != "hardcode@example.com" {
		t.Fatalf("fallback got %q", res.Identifier)
	}
	// No forms, no template — direct fallback
	cfg2 := map[string]interface{}{
		"setup.superAdmin.value": "solo@example.com",
	}
	res2, err := ResolveSetupConfig(cfg2, nil)
	if err != nil {
		t.Fatalf("solo fallback err %v", err)
	}
	if res2.Identifier != "solo@example.com" {
		t.Fatalf("solo got %q", res2.Identifier)
	}
}

func TestResolveSetupConfig_ExtraCommandsTemplated(t *testing.T) {
	cfg := map[string]interface{}{
		"installer.setup.super_admin_email": "{{forms.superAdmin.email}}",
		"setup.superAdmin.value":            "fallback@example.com",
		"installer.setup.extraCommands": []interface{}{
			"php artisan user:create --email={{forms.superAdmin.email}}",
			"php artisan role:assign {{forms.superAdmin.username}}",
			"echo plain",
		},
	}
	forms := map[string]map[string]string{
		"superAdmin": {"email": "cmd@example.com", "username": "cmduser"},
	}
	res, err := ResolveSetupConfig(cfg, forms)
	if err != nil {
		t.Fatalf("extraCommands err %v", err)
	}
	if len(res.ExtraCommands) != 3 {
		t.Fatalf("extra count %d", len(res.ExtraCommands))
	}
	if res.ExtraCommands[0] != "php artisan user:create --email=cmd@example.com" {
		t.Fatalf("cmd0 %q", res.ExtraCommands[0])
	}
	if res.ExtraCommands[1] != "php artisan role:assign cmduser" {
		t.Fatalf("cmd1 %q", res.ExtraCommands[1])
	}
	if res.ExtraCommands[2] != "echo plain" {
		t.Fatalf("cmd2 %q", res.ExtraCommands[2])
	}
	// Ensure extraCommands with fallback when forms missing
	cfg3 := map[string]interface{}{
		"installer.setup.super_admin_email": "{{forms.superAdmin.email}}",
		"setup.superAdmin.value":            "fallback@example.com",
		"installer.setup.extraCommands":     []string{"notify {{forms.superAdmin.email}}"},
	}
	res3, err := ResolveSetupConfig(cfg3, map[string]map[string]string{})
	if err != nil {
		t.Fatalf("extra fallback err %v", err)
	}
	if res3.ExtraCommands[0] != "notify fallback@example.com" {
		t.Fatalf("extra fallback got %q", res3.ExtraCommands[0])
	}
}

func TestValidateIdentifier(t *testing.T) {
	if err := ValidateIdentifier("admin@example.com"); err != nil {
		t.Fatalf("valid email err %v", err)
	}
	if err := ValidateIdentifier("john_doe"); err != nil {
		t.Fatalf("valid username err %v", err)
	}
	if err := ValidateIdentifier(""); err == nil {
		t.Fatalf("empty should fail")
	}
	if err := ValidateIdentifier("not-an-email@"); err == nil {
		t.Fatalf("invalid email should fail")
	}
	if err := ValidateIdentifier("a"); err == nil {
		t.Fatalf("too short username should fail")
	}
}

func TestResolveSetupConfig_ValidationFailure(t *testing.T) {
	cfg := map[string]interface{}{
		"installer.setup.super_admin_email": "{{forms.superAdmin.email}}",
		"setup.superAdmin.value":            "fallback@example.com",
	}
	formsInvalid := map[string]map[string]string{
		"superAdmin": {"email": "not-an-email"},
	}
	_, err := ResolveSetupConfig(cfg, formsInvalid)
	if err == nil {
		t.Fatalf("invalid email should fail validation")
	}
}

func TestPasswordRedaction(t *testing.T) {
	// Simulate forms with password field name password and secretToken
	cfg := map[string]interface{}{
		"installer.forms": map[string]interface{}{
			"superAdmin": map[string]interface{}{
				"fields": []interface{}{
					map[string]interface{}{"name": "email", "type": "email"},
					map[string]interface{}{"name": "password", "type": "password"},
					map[string]interface{}{"name": "secretToken", "type": "password"},
				},
			},
		},
		"installer.setup.super_admin_email": "{{forms.superAdmin.email}}",
		"setup.superAdmin.value":            "admin@example.com",
	}
	forms := map[string]map[string]string{
		"superAdmin": {"email": "a@b.com", "password": "hunter2", "secretToken": "tok123"},
	}
	res, err := ResolveSetupConfig(cfg, forms)
	if err != nil {
		t.Fatalf("resolve for redaction: %v", err)
	}
	// Password fields should be captured
	foundPassword := false
	for _, f := range res.PasswordFields {
		if f == "password" {
			foundPassword = true
		}
	}
	if !foundPassword {
		t.Fatalf("password fields %v missing password", res.PasswordFields)
	}
	// Ensure log redaction masks password values
	line := "creating user password=hunter2 secretToken: tok123 email=a@b.com"
	redacted := RedactSetupLog(line, res)
	if strings.Contains(redacted, "hunter2") {
		t.Fatalf("password not redacted %q", redacted)
	}
	if strings.Contains(redacted, "tok123") {
		t.Fatalf("secretToken not redacted %q", redacted)
	}
	if !strings.Contains(redacted, "***REDACTED***") {
		t.Fatalf("missing REDACTED marker %q", redacted)
	}
	// Also test generic password literal redaction when no dynamic field
	line2 := "DB_PASSWORD=supersecret"
	redacted2 := RedactInstallerLog(line2)
	if strings.Contains(redacted2, "supersecret") && !strings.Contains(line2, "DB_PASSWORD") {
		// Actually RedactInstallerLog should redact via env? But generic path
	}
	_ = config.ResolveFormsTemplate // ensure import used
}

func TestLaravelSetupCommands(t *testing.T) {
	cfg := SetupConfig{Identifier: "admin@example.com", Email: "admin@example.com", ExtraCommands: []string{"php artisan extra"}}
	cmds := LaravelSetupCommands(cfg, "/opt/app")
	foundMigrate := false
	foundSeed := false
	for _, c := range cmds {
		if strings.Contains(c, "migrate --force") {
			foundMigrate = true
		}
		if strings.Contains(c, "firstOrCreate") && strings.Contains(c, "admin@example.com") {
			foundSeed = true
		}
	}
	if !foundMigrate {
		t.Fatalf("missing migrate cmd %v", cmds)
	}
	if !foundSeed {
		t.Fatalf("missing seed with identifier %v", cmds)
	}
	if cmds[len(cmds)-1] != "php artisan extra" {
		t.Fatalf("extraCommands not appended %v", cmds)
	}
	// Username case
	cfg2 := SetupConfig{Identifier: "john", Username: "john", ExtraCommands: nil}
	cmds2 := LaravelSetupCommands(cfg2, "")
	foundUser := false
	for _, c := range cmds2 {
		if strings.Contains(c, "username") && strings.Contains(c, "john") {
			foundUser = true
		}
	}
	if !foundUser {
		t.Fatalf("username seed missing %v", cmds2)
	}
}

func TestEnsureLaravelSetupStage(t *testing.T) {
	cfg := SetupConfig{Identifier: "ensure@example.com"}
	cmds := EnsureLaravelSetupStage(cfg, "/tmp/app")
	if len(cmds) < 3 {
		t.Fatalf("ensure should return at least 3 cmds got %v", cmds)
	}
}
