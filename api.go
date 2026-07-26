package kingshot

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// API endpoints and signing key for the KingShot gift code service.
const (
	defaultLoginURL  = "https://kingshot-giftcode.centurygame.com/api/player"
	defaultRedeemURL = "https://kingshot-giftcode.centurygame.com/api/gift_code"
	Key              = "mN4!pQs6JrYwV9"
)

// Error codes returned by the KingShot API.
const (
	ErrCodeSuccess      = "20000"
	ErrCodeClaimed      = "40008"
	ErrCodeExpired      = "40007"
	ErrCodeNotFound     = "40014"
	ErrCodeLogin        = "40009"
	ErrCodeLimitReached = "40005"
	ErrCodeUserDetails  = "40020"
)

// transport is a rate-limited http.RoundTripper.
type transport struct {
	limiter *rate.Limiter
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, err
	}
	return http.DefaultTransport.RoundTrip(req)
}

// RedeemRequest holds the parameters for a single gift code redemption call.
type RedeemRequest struct {
	FID string // KingShot player ID
	KID string // kingdom ID
	CDK string // gift code
}

// redeemOutcome captures the structured result of a single redemption attempt.
type redeemOutcome struct {
	msg         string
	codeExpired bool
	codeInvalid bool
	loginFailed bool
}

// interpretRedeemResult maps a KingShot API error code to a structured outcome.
// This is a pure function — no I/O, no state.
func interpretRedeemResult(errCode ErrCode) redeemOutcome {
	switch string(errCode) {
	case ErrCodeSuccess:
		return redeemOutcome{msg: "Successfully redeemed!"}
	case ErrCodeClaimed:
		return redeemOutcome{msg: "Already claimed."}
	case ErrCodeExpired:
		return redeemOutcome{msg: "Code expired or not found.", codeExpired: true}
	case ErrCodeNotFound:
		return redeemOutcome{msg: "Code is not valid.", codeInvalid: true}
	case ErrCodeLogin:
		return redeemOutcome{msg: "Unable to login.", loginFailed: true}
	case ErrCodeLimitReached:
		return redeemOutcome{msg: "Redemption Limit Reached"}
	case ErrCodeUserDetails:
		return redeemOutcome{msg: "User details incorrect.", loginFailed: true}
	default:
		return redeemOutcome{msg: "Failed to redeem code."}
	}
}

// login authenticates fid with the KingShot API.
func (s *GiftCodeService) login(fid string) (*LoginResponse, error) {
	data := map[string]string{
		"fid":  fid,
		"time": fmt.Sprintf("%d", time.Now().Unix()),
	}
	payload, err := EncodePayload(data)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Post(s.loginURL, "application/json", strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, fmt.Errorf("failed to decode login response: %w", err)
	}
	return &loginResp, nil
}

// redeemGiftCode submits a redemption request using the parameters in req.
func (s *GiftCodeService) redeemGiftCode(req RedeemRequest) (*RedeemResponse, error) {
	data := map[string]string{
		"fid":  req.FID,
		"kid":  req.KID,
		"cdk":  req.CDK,
		"time": fmt.Sprintf("%d", time.Now().Unix()),
	}
	payload, err := EncodePayload(data)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Post(s.redeemURL, "application/json", strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var redeemResp RedeemResponse
	if err := json.NewDecoder(resp.Body).Decode(&redeemResp); err != nil {
		return nil, fmt.Errorf("failed to decode redemption response: %w", err)
	}
	return &redeemResp, nil
}

// EncodePayload encodes the data map into a signed JSON payload for the
// KingShot API. It adds a "sign" field to data as a side effect.
func EncodePayload(data map[string]string) (string, error) {
	values := url.Values{}
	for key, value := range data {
		values.Set(key, value)
	}

	hasher := md5.New()
	hasher.Write([]byte(values.Encode() + Key))
	data["sign"] = hex.EncodeToString(hasher.Sum(nil))

	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// ErrCode handles the KingShot API's habit of returning err_code as either a
// JSON string or a JSON number.
type ErrCode string

func (e *ErrCode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*e = ErrCode(s)
		return nil
	}
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*e = ErrCode(strconv.Itoa(i))
		return nil
	}
	return fmt.Errorf("err_code is not a string or a number: %s", data)
}

type LoginResponse struct {
	Code    int     `json:"code"`
	Message string  `json:"msg"`
	Data    any     `json:"data"`
	ErrCode ErrCode `json:"err_code"`
}

type RedeemResponse struct {
	Code    int     `json:"code"`
	Message string  `json:"msg"`
	ErrCode ErrCode `json:"err_code"`
}
