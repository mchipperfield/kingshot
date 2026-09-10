package kingshot

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// API endpoints and signing key for the KingShot gift code service.
const (
	defaultRedeemURL = "https://kingshot-giftcode.centurygame.com/api/gift_code"
	Key              = "mN4!pQs6JrYwV9"
)

// Known error codes returned by the KingShot API.
const (
	ErrCodeSuccess       = "20000"
	ErrCodeClaimed       = "40008"
	ErrCodeExpired       = "40007"
	ErrCodeNotFound      = "40014"
	ErrCodeLogin         = "40009"
	ErrCodeLimitReached  = "40005"
	ErrCodeTimeoutRetry  = "40004"
	ErrCodeUnknownPlayer = "40020"
)

var ErrTooManyRequests = errors.New(http.StatusText(http.StatusTooManyRequests))

// redeemResponse represents the response from the KingShot API when redeeming a gift code.
type redeemResponse struct {
	Message string `json:"msg"`
	ErrCode string `json:"err_code"`
}

func (r *redeemResponse) Error() string {
	return fmt.Sprintf("redeemResponse: err_code=%s, msg=%s", r.ErrCode, r.Message)
}

const maxRedeemAttempts = 3

// Client is a rate-limited HTTP client for interacting with the KingShot gift code service.
type Client struct {
	*http.Client
	redeemURL  string
	signingKey string
	logger     *slog.Logger
}

// NewClient creates a new Client with a rate limit of 1 request every 2 seconds, determined by the Kingshot API's x-rate-limit header.
// The client has an arbitrary 10-second timeout for requests, which seems reasonable.
// We may later take these values as parameters to make future changes easier.
// A nil logger falls back to slog.Default().
func NewClient(logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	limiter := rate.NewLimiter(rate.Limit(rate.Every(2*time.Second)), 1)
	return &Client{
		Client: &http.Client{
			Transport: &transport{limiter: limiter},
			Timeout:   10 * time.Second,
		},
		redeemURL:  defaultRedeemURL, // TODO: Make this configurable for testing purposes, or if the API endpoint changes in the future.
		signingKey: Key,              // TODO: Make this configurable for testing purposes, or if the signing key changes in the future.
		logger:     logger,
	}
}

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

// redeemGiftCode attempts to redeem a gift code for a player, retrying up to maxRedeemAttempts times if the response is retryable.
func (c *Client) redeemGiftCode(ctx context.Context, playerID, kingdomID, cdk string) (*redeemResponse, error) {
	data := map[string]string{
		"fid":  playerID,
		"kid":  kingdomID,
		"cdk":  cdk,
		"time": fmt.Sprintf("%d", time.Now().Unix()),
	}
	payload, err := encodePayload(data)
	if err != nil {
		return nil, err
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}

	for attempt := 1; attempt <= maxRedeemAttempts; attempt++ {
		redeemResp, err := c.redeem(ctx, payload)
		if err != nil {
			c.logger.Error("redemption failed", "attempt", attempt, "player_id", playerID, "code", cdk, "error", err)
			if ctx.Err() != nil {
				return nil, fmt.Errorf("client: redeem gift code: %w", ctx.Err())
			}
			if attempt < maxRedeemAttempts {
				c.logger.Info("retrying KingShot redemption", "attempt", attempt+1, "player_id", playerID, "code", cdk, "error", err)
			}
			continue
		}
		if redeemResp.ErrCode == ErrCodeTimeoutRetry {
			if attempt < maxRedeemAttempts {
				c.logger.Info("retrying KingShot redemption", "attempt", attempt+1, "player_id", playerID, "code", cdk, "error", redeemResp)
			}
			continue
		}
		return redeemResp, nil
	}

	return nil, fmt.Errorf("client: redemption attempts exhausted for player %s", playerID)
}

// redeemGiftCode submits a request with the API and decodes the response.
// A "successful" redemption means that the request succeeded and the API returned a valid response.
// The caller must interpret the ErrCode in the response to determine if the redemption was actually successful.
func (c *Client) redeem(ctx context.Context, payload string) (*redeemResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.redeemURL, strings.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("client: create redemption request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("client: send redemption request: %w", err)
	}
	defer resp.Body.Close()

	// Handle the case where the API returns a 429 Too Many Requests status code, which indicates that the client should retry after some time.
	// The API returns text/html, not application/json in the response so won't be able to decode it. In this case, we return a retryable error.
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrTooManyRequests
	}
	var redeemResp redeemResponse
	// So far,the API has responded with only 200 OK
	if err := json.NewDecoder(resp.Body).Decode(&redeemResp); err != nil {
		return nil, fmt.Errorf("client: decode redemption response: %w", err)
	}
	return &redeemResp, nil
}

// encodePayload encodes the data map into a signed JSON payload for the
// KingShot API. It adds a "sign" field to data as a side effect.
func encodePayload(data map[string]string) (string, error) {
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
