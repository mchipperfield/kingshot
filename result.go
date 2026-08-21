package kingshot

// RedeemResult describes a successfully processed gift code.
type RedeemResult struct {
	Code          string
	Added         bool
	PlayerResults []PlayerRedeemResult
}

// PlayerRedeemResult is the redemption outcome for a single player.
type PlayerRedeemResult struct {
	GuildID  string
	PlayerID string
	Message  string
}

// RegisterResult is the structured outcome of a RegisterPlayer call.
type RegisterResult struct {
	PlayerID                    string
	UserID                      string
	GuildID                     string
	KingdomID                   string
	AlreadySelf                 bool // already registered to this exact externalID
	AlreadyOther                bool // already registered to a different externalID
	InvalidPlayer               bool
	MaxPlayersForKingdomReached bool
	StoreError                  error
	APIError                    error
	CodeResults                 []RegistrationResult
	Success                     bool
}

// RegistrationResult is the redemption outcome for a single active code during registration.
type RegistrationResult struct {
	Code    string
	Message string
}

// TransferPlayerResult is the structured outcome of a TransferPlayer call.
type TransferPlayerResult struct {
	Player
	// RegistrationResult is populated if the player did not exist and was
	// registered instead.
	RegistrationResult *RegisterResult
}
