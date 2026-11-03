package capture

import "github.com/lambdawalker/go.appexfission.aws.card-capture/contracts"

type Purposes = contracts.Purposes

func DefaultPurposes() Purposes { return contracts.DefaultPurposes() }

func ValidOrigin(v string) bool {
	switch v {
	case "", "CA", "US", "MX", "GT", "BZ":
		return true
	}
	return false
}
