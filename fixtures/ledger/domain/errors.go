package domain

import "errors"

// ErrEmptyAccount is returned for an entry that names no account.
var ErrEmptyAccount = errors.New("ledger: an entry needs an account")

// ErrNegativeAmount is returned for an amount below zero.
var ErrNegativeAmount = errors.New("ledger: an amount cannot be negative")

// ErrUnknownAccount is returned when an account has no entries to describe.
var ErrUnknownAccount = errors.New("ledger: no such account")
