package extractor

import "testing"

func TestExtractJSONObject(t *testing.T) {
	ok := []struct {
		name, in, key, want string
	}{
		{"plain", `{"card_version":"cccd_chip_2021","id_number":"082279855585"}`, "id_number", "082279855585"},
		{"fenced", "```json\n{\"card_version\":\"can_cuoc_2024\"}\n```", "card_version", "can_cuoc_2024"},
		{"prose around", "Đây là kết quả:\n{\"full_name\":\"HOÀNG THÙY HUYỀN\"}\nHết.", "full_name", "HOÀNG THÙY HUYỀN"},
		{"brace inside value", `{"residence":"Số 3 {tầng 2}, Huế"}`, "residence", "Số 3 {tầng 2}, Huế"},
		{"leading junk brace", `{ broken {"sex":"Nữ"}`, "sex", "Nữ"},
	}
	for _, c := range ok {
		got, err := ExtractJSONObject(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got[c.key] != c.want {
			t.Fatalf("%s: %s = %v", c.name, c.key, got[c.key])
		}
	}

	// qwen3.8-flash with response_format echoed the schema: an object is found,
	// but it lacks card_version — the caller must treat it as unusable.
	echo := `[{"type": "object", "properties": {"so_cccd": {"type": "string"}}, "required": ["so_cccd"]}]`
	got, err := ExtractJSONObject(echo)
	if err != nil {
		t.Fatalf("schema echo: %v", err)
	}
	if _, has := got["card_version"]; has {
		t.Fatal("schema echo must not contain card_version")
	}

	for _, bad := range []string{"", "[]", "no json here", `{"a": "b", "c": `, `["card_version"]`} {
		if _, err := ExtractJSONObject(bad); err == nil {
			t.Fatalf("%q: expected error", bad)
		}
	}
}
