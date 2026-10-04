package domain

import "errors"

var ErrEmptyAccount = errors.New("ledger: an entry needs an account")

var ErrNegativeAmount = errors.New("ledger: an amount cannot be negative")

var ErrUnknownAccount = errors.New("ledger: no such account")
