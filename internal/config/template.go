package config

import (
	"regexp"
	"strings"
)

// formsTemplatePattern matches {{forms.<form>.<field>}}
var formsTemplatePattern = regexp.MustCompile(`\{\{\s*forms\.([a-zA-Z][a-zA-Z0-9_]*)\.([a-zA-Z][a-zA-Z0-9_]*)\s*\}\}`)

// ResolveFormsTemplate replaces {{forms.<form>.<field>}} placeholders using values map.
// values is map[formName]map[fieldName]value. If placeholder has no matching value, it is left unchanged
// unless fallback is provided (fallback is used when value missing).
// This implements SetupConfig template resolution for superAdmin identifier templated from form input.
func ResolveFormsTemplate(tmpl string, values map[string]map[string]string) string {
	return formsTemplatePattern.ReplaceAllStringFunc(tmpl, func(m string) string {
		matches := formsTemplatePattern.FindStringSubmatch(m)
		if len(matches) != 3 {
			return m
		}
		formName := matches[1]
		fieldName := matches[2]
		if formMap, ok := values[formName]; ok {
			if val, ok := formMap[fieldName]; ok {
				return val
			}
		}
		return m
	})
}

// ResolveFormsTemplateWithFallback replaces placeholders and falls back to fallbackValue when any placeholder remains unresolved.
// If template contains no placeholder, fallback is not used.
func ResolveFormsTemplateWithFallback(tmpl string, values map[string]map[string]string, fallback string) string {
	if !formsTemplatePattern.MatchString(tmpl) {
		return tmpl
	}
	resolved := ResolveFormsTemplate(tmpl, values)
	// if still contains placeholder (unresolved), return fallback if provided
	if fallback != "" && formsTemplatePattern.MatchString(resolved) {
		// only fallback if unresolved remains; but also keep resolved parts?
		// Spec: superAdmin identifier templated from form input (fallback hardcode value).
		// So if any field missing, fallback to hardcode.
		// We replace remaining placeholders with fallback.
		resolved = formsTemplatePattern.ReplaceAllString(resolved, fallback)
	}
	// If template was exactly one placeholder and unresolved, directly fallback
	if strings.TrimSpace(resolved) == "" && fallback != "" {
		return fallback
	}
	return resolved
}

// ContainsFormsPlaceholder reports whether string contains {{forms...}} placeholder.
func ContainsFormsPlaceholder(s string) bool {
	return formsTemplatePattern.MatchString(s)
}
