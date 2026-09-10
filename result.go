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
	Player
	CodeResults []RegistrationResult
}

// RegistrationResult is the redemption outcome for a single active code during registration.
type RegistrationResult struct {
	Code    string
	Message string
}
