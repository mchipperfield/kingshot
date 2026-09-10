package kingshot

import "errors"

// ErrNotFound is returned by store implementations when a requested record
// does not exist.
var ErrNotFound error = errors.New("not found")

// Errors returned by GiftCodeService for player registration, transfer, and
// unlink operations.
var (
	ErrAlreadySelf          = errors.New("player already registered to this user")
	ErrAlreadyOther         = errors.New("player already registered to a different user")
	ErrAlreadyInKingdom     = errors.New("player already in kingdom")
	ErrMaxPlayersForKingdom = errors.New("max players for kingdom reached")
	NotYourPlayer           = errors.New("not your player")
	ErrPlayerNotRegistered  = errors.New("player not registered")
)

// PlayerError is a caller-safe player registration or transfer failure.
// Error may be shown directly to a user.
type PlayerError struct {
	message string
	cause   error
}

func newPlayerError(message string, cause error) *PlayerError {
	return &PlayerError{message: message, cause: cause}
}

func (e *PlayerError) Error() string {
	return e.message
}

func (e *PlayerError) Unwrap() error {
	return e.cause
}

// codeErrorKind classifies why a gift code redemption failed.
type codeErrorKind uint8

const (
	codeErrorUnknown codeErrorKind = iota
	codeErrorActive
	codeErrorClaimed
	codeErrorExpired
	codeErrorInvalid
	codeErrorLogin
	codeErrorLimitReached
)

// CodeError is a caller-safe gift-code failure returned by GiftCodeService.
// Error may be shown directly to a user.
type CodeError struct {
	message string
	kind    codeErrorKind
	cause   error
}

func newCodeError(message string, kind codeErrorKind, cause error) *CodeError {
	return &CodeError{message: message, kind: kind, cause: cause}
}

func (e *CodeError) Error() string {
	return e.message
}

func (e *CodeError) Unwrap() error {
	return e.cause
}

// Errors returned by BearService.
var (
	ErrSetTimeInPast = errors.New("set time cannot be in the past")
	ErrInvalidBear   = errors.New("invalid bear trap")
)
