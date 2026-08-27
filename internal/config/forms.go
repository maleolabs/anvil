// Package config provides forms schema for installer pipeline core v3.
// Reference: ADR-005, sto:installer-pipeline-core
package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Allowed field types for installer.forms (6 types).
var AllowedFormFieldTypes = map[string]bool{
	"text":     true,
	"email":    true,
	"password": true,
	"select":   true,
	"number":   true,
	"textarea": true,
}

var fieldNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// WhenCondition represents conditional visibility: field==value.
type WhenCondition struct {
	Field string `json:"field" yaml:"field"`
	Value string `json:"value" yaml:"value"`
}

// FormField defines a single form field in installer.forms.
type FormField struct {
	Name         string         `json:"name" yaml:"name"`
	Type         string         `json:"type" yaml:"type"`
	Required     bool           `json:"required,omitempty" yaml:"required,omitempty"`
	MinLength    *int           `json:"minLength,omitempty" yaml:"minLength,omitempty"`
	Pattern      string         `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Confirmation string         `json:"confirmation,omitempty" yaml:"confirmation,omitempty"`
	When         *WhenCondition `json:"when,omitempty" yaml:"when,omitempty"`
	Options      []string       `json:"options,omitempty" yaml:"options,omitempty"`
	Label        string         `json:"label,omitempty" yaml:"label,omitempty"`
}

// InstallerForm defines one named form under installer.forms.
type InstallerForm struct {
	Fields []FormField `json:"fields" yaml:"fields"`
	Title  string      `json:"title,omitempty" yaml:"title,omitempty"`
}

// InstallerForms is map of formName -> InstallerForm
type InstallerForms map[string]InstallerForm

// ValidateFormsSchema validates installer.forms map per spec:
// - name unique within form, regex ^[a-zA-Z][a-zA-Z0-9_]*$
// - type allowlist 6
// - required/minLength/pattern/confirmation checks
// - when conditional field==value referencing existing field.
func ValidateFormsSchema(forms InstallerForms) []ValidationError {
	var errs []ValidationError
	for formName, form := range forms {
		if strings.TrimSpace(formName) == "" {
			errs = append(errs, ValidationError{
				Key:      "installer.forms",
				Expected: "form name must be non-empty",
				Actual:   formName,
			})
			continue
		}
		if !fieldNamePattern.MatchString(formName) {
			errs = append(errs, ValidationError{
				Key:      fmt.Sprintf("installer.forms.%s", formName),
				Expected: "form name must match ^[a-zA-Z][a-zA-Z0-9_]*$",
				Actual:   formName,
			})
		}
		seen := map[string]bool{}
		fieldNames := map[string]bool{}
		for _, f := range form.Fields {
			fieldNames[f.Name] = true
		}
		for i, f := range form.Fields {
			prefix := fmt.Sprintf("installer.forms.%s.fields[%d]", formName, i)
			if strings.TrimSpace(f.Name) == "" {
				errs = append(errs, ValidationError{
					Key:      prefix + ".name",
					Expected: "field name required and must match ^[a-zA-Z][a-zA-Z0-9_]*$",
					Actual:   f.Name,
				})
			} else if !fieldNamePattern.MatchString(f.Name) {
				errs = append(errs, ValidationError{
					Key:      prefix + ".name",
					Expected: "field name must match ^[a-zA-Z][a-zA-Z0-9_]*$",
					Actual:   f.Name,
				})
			} else if seen[f.Name] {
				errs = append(errs, ValidationError{
					Key:      prefix + ".name",
					Expected: "field name must be unique within form",
					Actual:   f.Name,
				})
			}
			seen[f.Name] = true

			if !AllowedFormFieldTypes[f.Type] {
				errs = append(errs, ValidationError{
					Key:      prefix + ".type",
					Expected: "type must be one of [text,email,password,select,number,textarea]",
					Actual:   f.Type,
				})
			}
			if f.MinLength != nil && *f.MinLength < 0 {
				errs = append(errs, ValidationError{
					Key:      prefix + ".minLength",
					Expected: "minLength must be >=0",
					Actual:   *f.MinLength,
				})
			}
			if f.Pattern != "" {
				if _, err := regexp.Compile(f.Pattern); err != nil {
					errs = append(errs, ValidationError{
						Key:      prefix + ".pattern",
						Expected: "pattern must be valid regexp",
						Actual:   f.Pattern,
					})
				}
			}
			if f.Confirmation != "" {
				if !fieldNames[f.Confirmation] {
					errs = append(errs, ValidationError{
						Key:      prefix + ".confirmation",
						Expected: "confirmation must reference existing field in same form",
						Actual:   f.Confirmation,
					})
				}
				if f.Confirmation == f.Name {
					errs = append(errs, ValidationError{
						Key:      prefix + ".confirmation",
						Expected: "confirmation must not reference self",
						Actual:   f.Confirmation,
					})
				}
			}
			if f.When != nil {
				if strings.TrimSpace(f.When.Field) == "" {
					errs = append(errs, ValidationError{
						Key:      prefix + ".when.field",
						Expected: "when.field must be non-empty and reference existing field",
						Actual:   f.When.Field,
					})
				} else if !fieldNames[f.When.Field] {
					errs = append(errs, ValidationError{
						Key:      prefix + ".when.field",
						Expected: "when.field must reference existing field in same form",
						Actual:   f.When.Field,
					})
				}
			}
			if f.Type == "select" {
				if len(f.Options) == 0 {
					errs = append(errs, ValidationError{
						Key:      prefix + ".options",
						Expected: "select field must have at least one option",
						Actual:   f.Options,
					})
				}
			}
		}
	}
	return errs
}

// ParseInstallerFormsFromFlat extracts installer.forms from flat config map.
func ParseInstallerFormsFromFlat(flat map[string]interface{}) (InstallerForms, error) {
	if flat == nil {
		return nil, nil
	}
	if raw, ok := flat["installer.forms"]; ok {
		return parseFormsRaw(raw)
	}
	prefix := "installer.forms."
	hasPrefix := false
	for k := range flat {
		if strings.HasPrefix(k, prefix) {
			hasPrefix = true
			break
		}
	}
	if !hasPrefix {
		return nil, nil
	}
	nested := map[string]interface{}{}
	for k, v := range flat {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		parts := strings.Split(rest, ".")
		cur := nested
		for i, p := range parts {
			if i == len(parts)-1 {
				cur[p] = v
			} else {
				if _, ok := cur[p]; !ok {
					cur[p] = map[string]interface{}{}
				}
				if m, ok := cur[p].(map[string]interface{}); ok {
					cur = m
				} else {
					nm := map[string]interface{}{}
					cur[p] = nm
					cur = nm
				}
			}
		}
	}
	return parseFormsRaw(nested)
}

func parseFormsRaw(raw interface{}) (InstallerForms, error) {
	if raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		return parseFormsMap(v)
	case InstallerForms:
		return v, nil
	case map[string]InstallerForm:
		out := InstallerForms{}
		for k, f := range v {
			out[k] = f
		}
		return out, nil
	default:
		return nil, fmt.Errorf("installer.forms must be object, got %T", raw)
	}
}

func parseFormsMap(m map[string]interface{}) (InstallerForms, error) {
	out := InstallerForms{}
	for formName, formRaw := range m {
		switch fv := formRaw.(type) {
		case map[string]interface{}:
			form := InstallerForm{}
			if title, ok := fv["title"].(string); ok {
				form.Title = title
			}
			if fieldsRaw, ok := fv["fields"]; ok {
				fields, err := parseFields(fieldsRaw)
				if err != nil {
					return nil, fmt.Errorf("form %q: %w", formName, err)
				}
				form.Fields = fields
			}
			out[formName] = form
		case InstallerForm:
			out[formName] = fv
		default:
			return nil, fmt.Errorf("form %q must be object, got %T", formName, formRaw)
		}
	}
	return out, nil
}

func parseFields(raw interface{}) ([]FormField, error) {
	switch v := raw.(type) {
	case []interface{}:
		var fields []FormField
		for i, e := range v {
			f, err := parseField(e)
			if err != nil {
				return nil, fmt.Errorf("fields[%d]: %w", i, err)
			}
			fields = append(fields, f)
		}
		return fields, nil
	case []FormField:
		return v, nil
	case []map[string]interface{}:
		var fields []FormField
		for i, m := range v {
			f, err := parseField(m)
			if err != nil {
				return nil, fmt.Errorf("fields[%d]: %w", i, err)
			}
			fields = append(fields, f)
		}
		return fields, nil
	default:
		return nil, fmt.Errorf("fields must be array, got %T", raw)
	}
}

func parseField(raw interface{}) (FormField, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return FormField{}, fmt.Errorf("field must be object, got %T", raw)
	}
	f := FormField{}
	if name, ok := m["name"].(string); ok {
		f.Name = name
	}
	if typ, ok := m["type"].(string); ok {
		f.Type = typ
	}
	if req, ok := m["required"]; ok {
		switch v := req.(type) {
		case bool:
			f.Required = v
		case string:
			f.Required = v == "true" || v == "1"
		}
	}
	if ml, ok := m["minLength"]; ok {
		switch v := ml.(type) {
		case int:
			c := v
			f.MinLength = &c
		case int64:
			c := int(v)
			f.MinLength = &c
		case float64:
			c := int(v)
			f.MinLength = &c
		}
	}
	if pat, ok := m["pattern"].(string); ok {
		f.Pattern = pat
	}
	if conf, ok := m["confirmation"].(string); ok {
		f.Confirmation = conf
	}
	if whenRaw, ok := m["when"]; ok {
		if wm, ok := whenRaw.(map[string]interface{}); ok {
			w := &WhenCondition{}
			if field, ok := wm["field"].(string); ok {
				w.Field = field
			}
			if val, ok := wm["value"].(string); ok {
				w.Value = val
			} else if val, ok := wm["value"]; ok {
				w.Value = fmt.Sprintf("%v", val)
			}
			f.When = w
		}
	}
	if opts, ok := m["options"]; ok {
		switch v := opts.(type) {
		case []interface{}:
			for _, o := range v {
				if s, ok := o.(string); ok {
					f.Options = append(f.Options, s)
				}
			}
		case []string:
			f.Options = v
		}
	}
	if label, ok := m["label"].(string); ok {
		f.Label = label
	}
	return f, nil
}

// MarshalFormsJSON returns forms.json bytes for embedding.
func MarshalFormsJSON(forms InstallerForms) ([]byte, error) {
	if forms == nil {
		return []byte("{}"), nil
	}
	return json.MarshalIndent(forms, "", "  ")
}

// PasswordFieldNames returns names of password fields for redaction.
func PasswordFieldNames(forms InstallerForms) []string {
	var out []string
	for _, form := range forms {
		for _, f := range form.Fields {
			if f.Type == "password" {
				out = append(out, f.Name)
			}
		}
	}
	return out
}
