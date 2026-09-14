package twofa

import "context"

// ServiceName is the public registration identifier for the 2FA cross-plugin service.
const ServiceName = "twofa.provider"

// Provider defines the executable machine contract exposed to other Octarq plugins
// and the core runtime.
type Provider interface {
	// GetCode retrieves the current valid TOTP code and remaining seconds for a given account.
	GetCode(ctx context.Context, orgID uint, accountName string) (code string, remainingSeconds int, err error)

	// VerifyCode checks whether a user-submitted code matches the current TOTP step for an account.
	VerifyCode(ctx context.Context, orgID uint, accountName string, code string) (bool, error)
}
