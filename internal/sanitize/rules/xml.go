// Package rules provides connector-specific sanitization rules.
package rules

import (
	"fmt"
	"regexp"
	"strings"
)

// xmlEntityPattern detects DOCTYPE declarations and entity references.
var xmlEntityPattern = regexp.MustCompile(`(?i)<!DOCTYPE|<!ENTITY|<!\[CDATA\[`)

// standardXMLEntities are the only allowed entity references.
var standardXMLEntities = map[string]bool{
	"&amp;":  true,
	"&lt;":   true,
	"&gt;":   true,
	"&quot;": true,
	"&apos;": true,
}

// XMLEntityRule blocks XML entity injection (XXE) in string values.
type XMLEntityRule struct{}

func (r *XMLEntityRule) Name() string { return "xml_entity_block" }

func (r *XMLEntityRule) Sanitize(value interface{}) (interface{}, error) {
	return sanitizeXMLValue(value)
}

func sanitizeXMLValue(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		if err := checkXMLEntities(v); err != nil {
			return nil, err
		}
		return v, nil

	case map[string]interface{}:
		result := make(map[string]interface{}, len(v))
		for key, val := range v {
			clean, err := sanitizeXMLValue(val)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", key, err)
			}
			result[key] = clean
		}
		return result, nil

	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			clean, err := sanitizeXMLValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = clean
		}
		return result, nil

	default:
		return value, nil
	}
}

// checkXMLEntities rejects strings containing DOCTYPE/ENTITY declarations
// or non-standard entity references that could trigger XXE.
func checkXMLEntities(s string) error {
	// Check for DOCTYPE and ENTITY declarations
	if xmlEntityPattern.MatchString(s) {
		return fmt.Errorf("XML entity/DOCTYPE declaration detected in input (potential XXE)")
	}

	// Check for entity references beyond the standard five
	idx := 0
	for idx < len(s) {
		ampPos := strings.IndexByte(s[idx:], '&')
		if ampPos == -1 {
			break
		}
		ampPos += idx

		semiPos := strings.IndexByte(s[ampPos:], ';')
		if semiPos == -1 {
			break
		}
		semiPos += ampPos

		entity := s[ampPos : semiPos+1]

		// Allow numeric character references (&#123; or &#x1F;)
		if strings.HasPrefix(entity, "&#") {
			idx = semiPos + 1
			continue
		}

		// Reject non-standard named entities
		if !standardXMLEntities[entity] {
			return fmt.Errorf("non-standard XML entity reference %q detected (potential XXE)", entity)
		}

		idx = semiPos + 1
	}

	return nil
}
