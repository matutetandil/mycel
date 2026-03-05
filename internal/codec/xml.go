package codec

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// XMLCodec encodes and decodes XML to/from map[string]interface{}.
//
// Conversion rules:
//   - Element name → map key, text content → string value
//   - Child elements → nested maps, repeated same-name elements → slices
//   - Attributes → "@attr" keys, text with attributes → "#text" key
//   - Root element name configurable (default: "root")
type XMLCodec struct {
	// RootElement is the name of the root XML element when encoding.
	RootElement string
}

func (c *XMLCodec) ContentType() string {
	return "application/xml"
}

func (c *XMLCodec) Name() string {
	return "xml"
}

// Encode converts a map to XML bytes.
func (c *XMLCodec) Encode(v interface{}) ([]byte, error) {
	root := c.RootElement
	if root == "" {
		root = "root"
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	if err := encodeElement(&buf, root, v, ""); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeElement writes a single XML element to the buffer.
func encodeElement(buf *bytes.Buffer, name string, v interface{}, indent string) error {
	switch val := v.(type) {
	case map[string]interface{}:
		buf.WriteString(indent)
		buf.WriteString("<")
		buf.WriteString(name)

		// Write attributes (keys starting with "@")
		var textContent interface{}
		hasChildren := false
		for k, child := range val {
			if strings.HasPrefix(k, "@") {
				attrName := k[1:]
				buf.WriteString(fmt.Sprintf(` %s="%s"`, attrName, xmlEscape(fmt.Sprintf("%v", child))))
			} else if k == "#text" {
				textContent = child
			} else {
				hasChildren = true
			}
		}

		// If only text content and attributes, write inline
		if !hasChildren && textContent != nil {
			buf.WriteString(">")
			buf.WriteString(xmlEscape(fmt.Sprintf("%v", textContent)))
			buf.WriteString("</")
			buf.WriteString(name)
			buf.WriteString(">\n")
			return nil
		}

		if !hasChildren && textContent == nil {
			buf.WriteString("/>\n")
			return nil
		}

		buf.WriteString(">\n")

		// Write child elements (skip attributes and #text)
		for k, child := range val {
			if strings.HasPrefix(k, "@") || k == "#text" {
				continue
			}
			if err := encodeElement(buf, k, child, indent+"  "); err != nil {
				return err
			}
		}

		// Write text content if present alongside children
		if textContent != nil {
			buf.WriteString(indent + "  ")
			buf.WriteString(xmlEscape(fmt.Sprintf("%v", textContent)))
			buf.WriteString("\n")
		}

		buf.WriteString(indent)
		buf.WriteString("</")
		buf.WriteString(name)
		buf.WriteString(">\n")

	case []interface{}:
		for _, item := range val {
			if err := encodeElement(buf, name, item, indent); err != nil {
				return err
			}
		}

	default:
		buf.WriteString(indent)
		buf.WriteString("<")
		buf.WriteString(name)
		if v == nil {
			buf.WriteString("/>\n")
		} else {
			buf.WriteString(">")
			buf.WriteString(xmlEscape(fmt.Sprintf("%v", val)))
			buf.WriteString("</")
			buf.WriteString(name)
			buf.WriteString(">\n")
		}
	}
	return nil
}

// xmlEscape escapes special XML characters.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return s
	}
	return buf.String()
}

// Decode parses XML bytes into a map.
func (c *XMLCodec) Decode(data []byte) (map[string]interface{}, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	result, err := decodeElement(decoder, nil)
	if err != nil {
		return nil, err
	}
	if m, ok := result.(map[string]interface{}); ok {
		return m, nil
	}
	return map[string]interface{}{"value": result}, nil
}

// decodeElement recursively decodes XML tokens into a map structure.
func decodeElement(decoder *xml.Decoder, start *xml.StartElement) (interface{}, error) {
	// If no start element provided, find the first one
	if start == nil {
		for {
			tok, err := decoder.Token()
			if err == io.EOF {
				return nil, fmt.Errorf("unexpected EOF")
			}
			if err != nil {
				return nil, err
			}
			if se, ok := tok.(xml.StartElement); ok {
				start = &se
				break
			}
		}
	}

	result := make(map[string]interface{})

	// Collect attributes
	for _, attr := range start.Attr {
		result["@"+attr.Name.Local] = attr.Value
	}

	// Collect children and text
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
			child, err := decodeElement(decoder, &t)
			if err != nil {
				return nil, err
			}
			childName := t.Name.Local

			// Handle repeated elements → slice
			if existing, ok := result[childName]; ok {
				switch ev := existing.(type) {
				case []interface{}:
					result[childName] = append(ev, child)
				default:
					result[childName] = []interface{}{ev, child}
				}
			} else {
				result[childName] = child
			}

		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text != "" {
				textParts = append(textParts, text)
			}

		case xml.EndElement:
			text := strings.Join(textParts, "")

			// If element has only text (no children, no attributes), return string
			if len(result) == 0 && text != "" {
				return text, nil
			}

			// If element has text plus other content, store as #text
			if text != "" {
				result["#text"] = text
			}

			// If element has no content at all, return empty string
			if len(result) == 0 && text == "" {
				return "", nil
			}

			return result, nil
		}
	}

	return result, nil
}
