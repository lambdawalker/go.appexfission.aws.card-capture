// Package runtimepolicy reads the shared, validated runtime consent policy.
package runtimepolicy

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/lambdawalker/go.appexfission.aws.card-capture/contracts"
)

var ErrInvalid = errors.New("invalid_config")
var ErrConflict = errors.New("revision_conflict")
var ErrForbidden = errors.New("environment_forbidden")
var ErrNotFound = errors.New("not_found")
var environmentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func ValidEnvironment(v string) bool { return environmentPattern.MatchString(v) }

type Features struct {
	IDCapture bool `json:"idCapture" pulumi:"idCapture"`
}
type Capture struct {
	Messages     contracts.Purposes `json:"messages" pulumi:"messages"`
	DocumentType string             `json:"documentType" pulumi:"documentType"`
	Jurisdiction string             `json:"jurisdiction" pulumi:"jurisdiction"`
	Purpose      string             `json:"purpose" pulumi:"purpose"`
}
type Config struct {
	SchemaVersion int         `json:"schemaVersion" pulumi:"schemaVersion"`
	Environment   string      `json:"environment" pulumi:"environment"`
	Revision      int64       `json:"revision" pulumi:"revision"`
	UpdatedAt     string      `json:"updatedAt" pulumi:"updatedAt"`
	Onboarding    *Onboarding `json:"onboarding,omitempty" pulumi:"onboarding"`
	Features      Features    `json:"features" pulumi:"features"`
	Capture       Capture     `json:"capture" pulumi:"capture"`
}

func Default(env string) Config {
	policy := DefaultOnboarding()
	return Config{Onboarding: &policy, Features: Features{IDCapture: true}, SchemaVersion: 1, Environment: env, UpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339), Capture: Capture{DocumentType: "id_card", Purpose: contracts.DefaultPurposes().EN, Messages: contracts.DefaultPurposes()}}
}
func (c Config) Validate() error {
	if c.EffectiveOnboarding().Validate() != nil || (c.Onboarding != nil && c.Features.IDCapture != (c.Onboarding.IDCapture != Disabled)) {
		return ErrInvalid
	}
	if c.SchemaVersion != 1 || !ValidEnvironment(c.Environment) || c.Revision < 0 {
		return ErrInvalid
	}
	if _, e := time.Parse(time.RFC3339, c.UpdatedAt); e != nil {
		return ErrInvalid
	}
	for _, v := range []string{c.Capture.DocumentType, c.Capture.Purpose, c.Capture.Jurisdiction, c.Capture.Messages.EN, c.Capture.Messages.ES, c.Capture.Messages.FR} {
		if len(v) > 1024 || strings.TrimSpace(v) != v || strings.ContainsFunc(v, unicode.IsControl) {
			return ErrInvalid
		}
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`).MatchString(c.Capture.DocumentType) || (c.Features.IDCapture && (c.Capture.DocumentType == "sample_card" || c.Capture.Purpose == "")) {
		return ErrInvalid
	}
	if c.Capture.Messages != (contracts.Purposes{}) && c.Capture.Messages.EN == "" {
		return ErrInvalid
	}
	return nil
}
func (c Config) Policy() contracts.Policy {
	messages := c.Capture.Messages
	if messages.EN == "" {
		messages = contracts.DefaultPurposes()
	}
	return contracts.Policy{Enabled: c.Features.IDCapture, DocumentType: "id_card", Purpose: messages.EN, Purposes: messages, Slots: []string{"front", "back"}, ContentType: "image/jpeg", MaxBytes: contracts.MaxBytes, RetentionDays: 7}.WithVersion()
}
