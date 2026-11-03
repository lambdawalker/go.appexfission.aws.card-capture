package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestConsentWireAndDigest(t *testing.T) {
	p := Policy{Purposes: DefaultPurposes(), Enabled: true, DocumentType: "id_card", Slots: []string{"front", "back"}, ContentType: "image/jpeg", MaxBytes: MaxBytes, Purpose: DefaultPurposes().EN, RetentionDays: 7}
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	got := p.WithVersion()
	if got.Version != "capture-sha256-2f02802cbbef290b0e6e87049fd22ad1a9473b04e7dc4e0ad4c3e828fb7c0109" {
		t.Fatal("consent wire/hash changed", got.Version)
	}
	if got.Version != "capture-sha256-"+hex.EncodeToString(sum[:]) {
		t.Fatal(got.Version)
	}
	if got.WithVersion().Version != got.Version {
		t.Fatal("version not cleared before digest")
	}
	for _, field := range []string{"purposes", "enabled", "policy_version", "document_type", "required_slots", "content_type", "max_bytes", "purpose", "jurisdiction", "retention_days"} {
		if !strings.Contains(string(b), `"`+field+`":`) {
			t.Fatal("missing wire field", field)
		}
	}
	if MaxBytes != 4194304 {
		t.Fatal(MaxBytes)
	}
}
