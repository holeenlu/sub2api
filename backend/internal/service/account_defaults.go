package service

import "github.com/Wei-Shaw/sub2api/internal/domain"

const DefaultAccountConcurrency = domain.DefaultAccountConcurrency
const DefaultAccountPriority = domain.DefaultAccountPriority

type AccountCreationDefaults struct {
	Concurrency int `json:"concurrency"`
	Priority    int `json:"priority"`
}

func NewAccountCreationDefaults() AccountCreationDefaults {
	return AccountCreationDefaults{Concurrency: DefaultAccountConcurrency, Priority: DefaultAccountPriority}
}
