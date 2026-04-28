package connector

import "testing"

func TestIntFromProps(t *testing.T) {
	tests := []struct {
		name       string
		props      map[string]interface{}
		key        string
		defaultVal int
		want       int
	}{
		{"int", map[string]interface{}{"port": 5432}, "port", 0, 5432},
		{"int64", map[string]interface{}{"port": int64(5432)}, "port", 0, 5432},
		{"float64", map[string]interface{}{"port": float64(5432)}, "port", 0, 5432},
		{"numeric string", map[string]interface{}{"port": "5432"}, "port", 0, 5432},
		{"missing", map[string]interface{}{}, "port", 5432, 5432},
		{"nil props", nil, "port", 5432, 5432},
		{"empty string falls back", map[string]interface{}{"port": ""}, "port", 5432, 5432},
		{"non-numeric string falls back", map[string]interface{}{"port": "abc"}, "port", 5432, 5432},
		{"nil value falls back", map[string]interface{}{"port": nil}, "port", 5432, 5432},
		{"unsupported type falls back", map[string]interface{}{"port": []int{1}}, "port", 5432, 5432},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IntFromProps(tc.props, tc.key, tc.defaultVal)
			if got != tc.want {
				t.Errorf("IntFromProps(%v, %q, %d) = %d, want %d", tc.props, tc.key, tc.defaultVal, got, tc.want)
			}
		})
	}
}

func TestIntFromPropsStrict(t *testing.T) {
	tests := []struct {
		name   string
		props  map[string]interface{}
		key    string
		want   int
		wantOK bool
	}{
		{"present int", map[string]interface{}{"port": 5432}, "port", 5432, true},
		{"present string", map[string]interface{}{"port": "5432"}, "port", 5432, true},
		{"missing", map[string]interface{}{}, "port", 0, false},
		{"non-numeric string", map[string]interface{}{"port": "abc"}, "port", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := IntFromPropsStrict(tc.props, tc.key)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("IntFromPropsStrict(%v, %q) = (%d, %v), want (%d, %v)", tc.props, tc.key, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
