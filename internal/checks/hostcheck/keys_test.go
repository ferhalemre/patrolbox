package hostcheck

import (
	"encoding/json"
	"os"
	"testing"
)

func TestMessageKeysExist(t *testing.T) {
	raw, err := os.ReadFile("../../i18n/locales/en/hostcheck.json")
	if err != nil {
		t.Fatal(err)
	}
	var cat map[string]string
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, k := range allKeys {
		seen[k] = true
		if cat[k] == "" {
			t.Errorf("key %q missing from hostcheck.json", k)
		}
	}
	for k := range cat {
		if !seen[k] {
			t.Errorf("hostcheck.json has unused key %q", k)
		}
	}
}
