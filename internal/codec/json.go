package codec

import "encoding/json"

// JSONCodec encodes and decodes JSON.
type JSONCodec struct{}

func (c *JSONCodec) Encode(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (c *JSONCodec) Decode(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *JSONCodec) ContentType() string {
	return "application/json"
}

func (c *JSONCodec) Name() string {
	return "json"
}
