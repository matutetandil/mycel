// Package soap provides a SOAP connector for calling and exposing SOAP web services.
package soap

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

const (
	// SOAP 1.1 namespace and content type.
	NS11          = "http://schemas.xmlsoap.org/soap/envelope/"
	ContentType11 = "text/xml; charset=utf-8"

	// SOAP 1.2 namespace and content type.
	NS12          = "http://www.w3.org/2003/05/soap-envelope"
	ContentType12 = "application/soap+xml; charset=utf-8"
)

// Fault represents a SOAP fault.
type Fault struct {
	Code   string
	String string
	Detail string
}

func (f *Fault) Error() string {
	if f.Detail != "" {
		return fmt.Sprintf("SOAP Fault: [%s] %s — %s", f.Code, f.String, f.Detail)
	}
	return fmt.Sprintf("SOAP Fault: [%s] %s", f.Code, f.String)
}

// soapNS returns the SOAP namespace for the given version.
func soapNS(version string) string {
	if version == "1.2" {
		return NS12
	}
	return NS11
}

// ContentTypeForVersion returns the Content-Type for the given SOAP version.
func ContentTypeForVersion(version string) string {
	if version == "1.2" {
		return ContentType12
	}
	return ContentType11
}

// Envelope wraps a body in a SOAP envelope.
// namespace is the service namespace used on the operation element.
func Envelope(version, namespace, operation string, body map[string]interface{}) ([]byte, error) {
	ns := soapNS(version)

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(fmt.Sprintf(`<soap:Envelope xmlns:soap="%s"`, ns))
	if namespace != "" {
		buf.WriteString(fmt.Sprintf(` xmlns:ns="%s"`, namespace))
	}
	buf.WriteString(">\n")
	buf.WriteString("  <soap:Body>\n")

	// Write operation element with body fields
	prefix := "ns:"
	if namespace == "" {
		prefix = ""
	}
	buf.WriteString(fmt.Sprintf("    <%s%s>\n", prefix, operation))
	writeMapAsXML(&buf, body, "      ")
	buf.WriteString(fmt.Sprintf("    </%s%s>\n", prefix, operation))

	buf.WriteString("  </soap:Body>\n")
	buf.WriteString("</soap:Envelope>")

	return buf.Bytes(), nil
}

// writeMapAsXML writes map fields as XML elements.
func writeMapAsXML(buf *bytes.Buffer, m map[string]interface{}, indent string) {
	for k, v := range m {
		switch val := v.(type) {
		case map[string]interface{}:
			buf.WriteString(fmt.Sprintf("%s<%s>\n", indent, k))
			writeMapAsXML(buf, val, indent+"  ")
			buf.WriteString(fmt.Sprintf("%s</%s>\n", indent, k))
		case []interface{}:
			for _, item := range val {
				if m2, ok := item.(map[string]interface{}); ok {
					buf.WriteString(fmt.Sprintf("%s<%s>\n", indent, k))
					writeMapAsXML(buf, m2, indent+"  ")
					buf.WriteString(fmt.Sprintf("%s</%s>\n", indent, k))
				} else {
					buf.WriteString(fmt.Sprintf("%s<%s>%s</%s>\n", indent, k, xmlEscape(fmt.Sprintf("%v", item)), k))
				}
			}
		default:
			if v == nil {
				buf.WriteString(fmt.Sprintf("%s<%s/>\n", indent, k))
			} else {
				buf.WriteString(fmt.Sprintf("%s<%s>%s</%s>\n", indent, k, xmlEscape(fmt.Sprintf("%v", val)), k))
			}
		}
	}
}

// xmlEscape escapes special XML characters.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// Unwrap extracts the operation name and body from a SOAP envelope.
// Returns fault if the response contains a SOAP fault.
func Unwrap(data []byte) (operation string, body map[string]interface{}, fault *Fault, err error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	// Navigate to soap:Body
	if err := navigateToBody(decoder); err != nil {
		return "", nil, nil, fmt.Errorf("failed to find SOAP Body: %w", err)
	}

	// Read first child of Body (the operation element or Fault)
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return "", nil, nil, fmt.Errorf("empty SOAP Body")
		}
		if err != nil {
			return "", nil, nil, err
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		localName := se.Name.Local

		// Check for SOAP Fault
		if localName == "Fault" {
			f, err := parseFault(decoder)
			if err != nil {
				return "", nil, nil, err
			}
			return "", nil, f, nil
		}

		// This is the operation element — strip "Response" suffix if present
		opName := localName
		opName = strings.TrimSuffix(opName, "Response")

		// Parse body content as map
		bodyMap, err := parseElementContent(decoder)
		if err != nil {
			return "", nil, nil, fmt.Errorf("failed to parse operation body: %w", err)
		}

		return opName, bodyMap, nil, nil
	}
}

// navigateToBody moves the decoder to the first child of soap:Body.
func navigateToBody(decoder *xml.Decoder) error {
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return fmt.Errorf("unexpected EOF before Body")
		}
		if err != nil {
			return err
		}

		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "Body" {
				return nil
			}
		}
	}
}

// parseFault parses a SOAP Fault element.
func parseFault(decoder *xml.Decoder) (*Fault, error) {
	fault := &Fault{}

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "faultcode", "Code":
				fault.Code = readText(decoder)
			case "faultstring", "Reason", "Text":
				fault.String = readText(decoder)
			case "detail", "Detail":
				fault.Detail = readText(decoder)
			default:
				decoder.Skip()
			}
		case xml.EndElement:
			if t.Name.Local == "Fault" {
				return fault, nil
			}
		}
	}

	return fault, nil
}

// readText reads text content until the matching end element.
func readText(decoder *xml.Decoder) string {
	var parts []string
	depth := 1
	for depth > 0 {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			parts = append(parts, string(t))
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return strings.TrimSpace(strings.Join(parts, ""))
}

// parseElementContent parses child elements of the current element into a map.
func parseElementContent(decoder *xml.Decoder) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	var textParts []string

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			child, err := parseElementValue(decoder, &t)
			if err != nil {
				return nil, err
			}

			// Handle repeated elements
			if existing, ok := result[t.Name.Local]; ok {
				switch ev := existing.(type) {
				case []interface{}:
					result[t.Name.Local] = append(ev, child)
				default:
					result[t.Name.Local] = []interface{}{ev, child}
				}
			} else {
				result[t.Name.Local] = child
			}

		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text != "" {
				textParts = append(textParts, text)
			}

		case xml.EndElement:
			text := strings.Join(textParts, "")
			if len(result) == 0 && text != "" {
				return map[string]interface{}{"#text": text}, nil
			}
			if text != "" {
				result["#text"] = text
			}
			return result, nil
		}
	}

	return result, nil
}

// parseElementValue parses an element's value (string for leaf, map for complex).
func parseElementValue(decoder *xml.Decoder, start *xml.StartElement) (interface{}, error) {
	var textParts []string
	children := make(map[string]interface{})

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			child, err := parseElementValue(decoder, &t)
			if err != nil {
				return nil, err
			}
			if existing, ok := children[t.Name.Local]; ok {
				switch ev := existing.(type) {
				case []interface{}:
					children[t.Name.Local] = append(ev, child)
				default:
					children[t.Name.Local] = []interface{}{ev, child}
				}
			} else {
				children[t.Name.Local] = child
			}

		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text != "" {
				textParts = append(textParts, text)
			}

		case xml.EndElement:
			text := strings.Join(textParts, "")
			// Leaf element: return text
			if len(children) == 0 {
				return text, nil
			}
			// Complex element: return map
			if text != "" {
				children["#text"] = text
			}
			return children, nil
		}
	}

	return strings.Join(textParts, ""), nil
}

// FaultEnvelope builds a complete SOAP fault response envelope.
func FaultEnvelope(version, code, message, detail string) []byte {
	ns := soapNS(version)

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(fmt.Sprintf(`<soap:Envelope xmlns:soap="%s">`, ns))
	buf.WriteString("\n  <soap:Body>\n    <soap:Fault>\n")

	if version == "1.2" {
		buf.WriteString(fmt.Sprintf("      <soap:Code><soap:Value>%s</soap:Value></soap:Code>\n", xmlEscape(code)))
		buf.WriteString(fmt.Sprintf("      <soap:Reason><soap:Text>%s</soap:Text></soap:Reason>\n", xmlEscape(message)))
		if detail != "" {
			buf.WriteString(fmt.Sprintf("      <soap:Detail>%s</soap:Detail>\n", xmlEscape(detail)))
		}
	} else {
		buf.WriteString(fmt.Sprintf("      <faultcode>%s</faultcode>\n", xmlEscape(code)))
		buf.WriteString(fmt.Sprintf("      <faultstring>%s</faultstring>\n", xmlEscape(message)))
		if detail != "" {
			buf.WriteString(fmt.Sprintf("      <detail>%s</detail>\n", xmlEscape(detail)))
		}
	}

	buf.WriteString("    </soap:Fault>\n  </soap:Body>\n</soap:Envelope>")
	return buf.Bytes()
}
