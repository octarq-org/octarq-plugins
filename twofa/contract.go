package twofa

import "context"

const ServiceName = "twofa.provider"

type Provider interface {
	GetCode(ctx context.Context, orgID uint, accountName string) (code string, remainingSeconds int, err error)

	VerifyCode(ctx context.Context, orgID uint, accountName string, code string) (bool, error)
}
