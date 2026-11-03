package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const MaxBytes int64 = 4 * 1024 * 1024

type Policy struct {
	Purposes      Purposes `json:"purposes,omitempty"`
	Enabled       bool     `json:"enabled"`
	Version       string   `json:"policy_version"`
	DocumentType  string   `json:"document_type"`
	Slots         []string `json:"required_slots"`
	ContentType   string   `json:"content_type"`
	MaxBytes      int64    `json:"max_bytes"`
	Purpose       string   `json:"purpose"`
	Jurisdiction  string   `json:"jurisdiction"`
	RetentionDays int      `json:"retention_days"`
}

// Purposes has a bounded language set; country-specific messages are deliberately unsupported.
type Purposes struct {
	EN string `json:"en" pulumi:"en"`
	ES string `json:"es" pulumi:"es"`
	FR string `json:"fr" pulumi:"fr"`
}

func DefaultPurposes() Purposes {
	return Purposes{
		EN: "Scan your ID to provide your identity details. You'll review and confirm the information afterward.",
		ES: "Escanea tu identificación para proporcionar tus datos de identidad. Después podrás revisar y confirmar la información.",
		FR: "Numérisez votre pièce d’identité pour fournir vos renseignements d’identité. Vous pourrez ensuite les vérifier et les confirmer.",
	}
}
func (p Purposes) Language(language string) string {
	if language == "es" && p.ES != "" {
		return "es"
	}
	if language == "fr" && p.FR != "" {
		return "fr"
	}
	return "en"
}
func (p Purposes) Text(language string) string {
	switch p.Language(language) {
	case "es":
		return p.ES
	case "fr":
		return p.FR
	}
	return p.EN
}

// WithVersion binds every consent-relevant field, including deployment defaults.
func (p Policy) WithVersion() Policy {
	p.Version = ""
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	p.Version = "capture-sha256-" + hex.EncodeToString(sum[:])
	return p
}
