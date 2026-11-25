package netcheck

import (
	"encoding/json"
	"os"
	"testing"
)

func TestKeysExistInCatalog(t *testing.T) {
	raw, err := os.ReadFile("../../i18n/locales/en/netcheck.json")
	if err != nil {
		t.Fatal(err)
	}
	var cat map[string]string
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	for _, k := range allKeys {
		used[k] = true
		if cat[k] == "" {
			t.Errorf("key %q missing from netcheck.json", k)
		}
	}
	for k := range cat {
		if !used[k] {
			t.Errorf("netcheck.json has unused key %q", k)
		}
	}
}
