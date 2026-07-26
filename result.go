package kingshot

// CodeResult is the structured outcome of a ProcessNewCode call.
type CodeResult struct {
	Code           string
	AlreadyActive  bool
	AlreadyExpired bool
	Invalid        bool
	LoginFailed    bool
	StoreError     error
	APIError       error
	Added          bool
	PlayerResults  []PlayerRedeemResult
}

// PlayerRedeemResult is the redemption outcome for a single player.
type PlayerRedeemResult struct {
	PlayerID string
	Message  string
}

// RegisterResult is the structured outcome of a RegisterPlayer call.
type RegisterResult struct {
	PlayerID     string
	ExternalID   string
	AlreadySelf  bool // already registered to this exact externalID
	AlreadyOther bool // already registered to a different externalID
	InvalidPlayer bool
	StoreError   error
	APIError     error
	CodeResults  []ActiveCodeResult
	Success      bool
}

// ActiveCodeResult is the redemption outcome for a single active code during registration.
type ActiveCodeResult struct {
	Code    string
	Message string
}
