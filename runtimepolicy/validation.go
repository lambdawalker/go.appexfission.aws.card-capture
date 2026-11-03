package runtimepolicy

// Requirement governs enrollment, never revocation of an existing credential.
type Requirement string

const (
	Required Requirement = "required"
	Optional Requirement = "optional"
	Disabled Requirement = "disabled"
)

type CharacterRule string

const (
	Allowed           CharacterRule = "allowed"
	Disallowed        CharacterRule = "disallowed"
	CharacterRequired CharacterRule = "required"
)

type PasswordPolicy struct {
	MinLength int           `json:"minLength" pulumi:"minLength"`
	MaxLength int           `json:"maxLength" pulumi:"maxLength"`
	Lowercase CharacterRule `json:"lowercase" pulumi:"lowercase"`
	Uppercase CharacterRule `json:"uppercase" pulumi:"uppercase"`
	Numbers   CharacterRule `json:"numbers" pulumi:"numbers"`
	Symbols   CharacterRule `json:"symbols" pulumi:"symbols"`
}
type Onboarding struct {
	IDCapture         Requirement    `json:"idCapture" pulumi:"idCapture"`
	Passkey           Requirement    `json:"passkey" pulumi:"passkey"`
	Password          Requirement    `json:"password" pulumi:"password"`
	PasswordPlacement string         `json:"passwordPlacement" pulumi:"passwordPlacement"`
	PasswordPolicy    PasswordPolicy `json:"passwordPolicy" pulumi:"passwordPolicy"`
}

func DefaultOnboarding() Onboarding {
	return Onboarding{Required, Required, Disabled, "hidden", PasswordPolicy{12, 128, Allowed, Allowed, Allowed, Allowed}}
}
func (c Config) EffectiveOnboarding() Onboarding {
	if c.Onboarding != nil {
		return *c.Onboarding
	}
	p := DefaultOnboarding()
	p.IDCapture = Disabled
	if c.Features.IDCapture {
		p.IDCapture = Optional
	}
	return p
}
func (c Config) Normalized() Config {
	p := c.EffectiveOnboarding()
	c.Onboarding = &p
	return c
}
func (p Onboarding) Validate() error {
	for _, r := range []Requirement{p.IDCapture, p.Passkey, p.Password} {
		if r != Required && r != Optional && r != Disabled {
			return ErrInvalid
		}
	}
	if p.PasswordPlacement != "hidden" && p.PasswordPlacement != "with_email" && p.PasswordPlacement != "after_verification" {
		return ErrInvalid
	}
	if (p.Password == Disabled) != (p.PasswordPlacement == "hidden") {
		return ErrInvalid
	}
	return p.PasswordPolicy.Validate()
}
func (p PasswordPolicy) rules() []CharacterRule {
	return []CharacterRule{p.Lowercase, p.Uppercase, p.Numbers, p.Symbols}
}
func (p PasswordPolicy) Validate() error {
	if p.MinLength < 8 || p.MaxLength > 128 || p.MinLength > p.MaxLength {
		return ErrInvalid
	}
	enabled := false
	for _, r := range p.rules() {
		if r != Allowed && r != CharacterRequired && r != Disallowed {
			return ErrInvalid
		}
		enabled = enabled || r != Disallowed
	}
	if !enabled {
		return ErrInvalid
	}
	return nil
}
