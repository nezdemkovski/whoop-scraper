package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	AuthorizeURL = "https://api.prod.whoop.com/oauth/oauth2/auth"
	TokenURL     = "https://api.prod.whoop.com/oauth/oauth2/token"
)

var (
	AllScopes   = []string{"offline", "read:profile", "read:body_measurement", "read:cycles", "read:recovery", "read:sleep", "read:workout"}
	ErrNoTokens = errors.New("no tokens available")
)

type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

func (t Tokens) Expired() bool {
	return time.Now().UTC().After(t.ExpiresAt.Add(-5 * time.Minute))
}

type TokenStorage interface {
	Save(context.Context, Tokens) error
	Load(context.Context) (*Tokens, error)
	Clear(context.Context) error
}

type Client struct {
	clientID     string
	clientSecret string
	storage      TokenStorage
	httpClient   *http.Client
	logger       *slog.Logger
	tokens       *Tokens
}

func New(clientID, clientSecret string, storage TokenStorage, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		storage:      storage,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		logger:       logger,
	}
}

func (c *Client) AuthorizationURL(redirectURI string) (string, string, error) {
	state, err := randomState()
	if err != nil {
		return "", "", err
	}
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", c.clientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("scope", strings.Join(AllScopes, " "))
	values.Set("state", state)
	return AuthorizeURL + "?" + values.Encode(), state, nil
}

func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI string) (Tokens, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	values.Set("client_id", c.clientID)
	values.Set("client_secret", c.clientSecret)
	return c.postToken(ctx, values)
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if refreshToken == "" {
		tokens, err := c.storage.Load(ctx)
		if err != nil {
			return Tokens{}, err
		}
		if tokens == nil {
			return Tokens{}, ErrNoTokens
		}
		refreshToken = tokens.RefreshToken
	}
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", c.clientID)
	values.Set("client_secret", c.clientSecret)
	values.Set("scope", strings.Join(AllScopes, " "))
	return c.postToken(ctx, values)
}

func (c *Client) ValidAccessToken(ctx context.Context) (string, error) {
	if c.tokens != nil && !c.tokens.Expired() {
		return c.tokens.AccessToken, nil
	}
	tokens, err := c.storage.Load(ctx)
	if err != nil {
		return "", err
	}
	if tokens == nil {
		return "", ErrNoTokens
	}
	if tokens.Expired() {
		refreshed, err := c.Refresh(ctx, tokens.RefreshToken)
		if err != nil {
			return "", err
		}
		tokens = &refreshed
	}
	c.tokens = tokens
	return tokens.AccessToken, nil
}

func (c *Client) AuthorizeInteractive(ctx context.Context, port int, openBrowser bool) (Tokens, error) {
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)
	authURL, expectedState, err := c.AuthorizationURL(redirectURI)
	if err != nil {
		return Tokens{}, err
	}

	result := make(chan callbackResult, 1)
	server := &http.Server{
		Addr:              fmt.Sprintf("localhost:%d", port),
		ReadHeaderTimeout: 5 * time.Second,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errValue := q.Get("error"); errValue != "" {
			result <- callbackResult{err: fmt.Errorf("authorization failed: %s", errValue)}
			_, _ = w.Write([]byte("<html><body><h1>Authorization denied. You can close this window.</h1></body></html>"))
			return
		}
		result <- callbackResult{code: q.Get("code"), state: q.Get("state")}
		_, _ = w.Write([]byte("<html><body><h1>Authorization successful. You can close this window.</h1></body></html>"))
	})
	server.Handler = mux

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return Tokens{}, err
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.logger.Error("oauth callback server failed", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("Visit this URL to authorize:\n%s\n", authURL)
	if openBrowser {
		_ = openURL(authURL)
	}

	select {
	case <-ctx.Done():
		return Tokens{}, ctx.Err()
	case <-time.After(5 * time.Minute):
		return Tokens{}, errors.New("authorization timed out")
	case res := <-result:
		if res.err != nil {
			return Tokens{}, res.err
		}
		if res.code == "" {
			return Tokens{}, errors.New("authorization callback did not include code")
		}
		if res.state != expectedState {
			return Tokens{}, errors.New("oauth state mismatch")
		}
		return c.ExchangeCode(ctx, res.code, redirectURI)
	}
}

func (c *Client) postToken(ctx context.Context, values url.Values) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Tokens{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if len(body) > 0 {
			return Tokens{}, fmt.Errorf("token request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return Tokens{}, fmt.Errorf("token request failed: %s", resp.Status)
	}

	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Tokens{}, err
	}
	if raw.ExpiresIn == 0 {
		raw.ExpiresIn = 3600
	}
	if raw.TokenType == "" {
		raw.TokenType = "bearer"
	}
	tokens := Tokens{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		ExpiresAt:    time.Now().UTC().Add(time.Duration(raw.ExpiresIn) * time.Second),
		TokenType:    raw.TokenType,
	}
	if err := c.storage.Save(ctx, tokens); err != nil {
		return Tokens{}, err
	}
	c.tokens = &tokens
	return tokens, nil
}

type callbackResult struct {
	code  string
	state string
	err   error
}

type FileStorage struct {
	path         string
	accessToken  string
	refreshToken string
}

func NewFileStorage(path, accessToken, refreshToken string) *FileStorage {
	return &FileStorage{path: path, accessToken: accessToken, refreshToken: refreshToken}
}

func (s *FileStorage) Save(_ context.Context, tokens Tokens) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(s.path, 0o600)
}

func (s *FileStorage) Load(_ context.Context) (*Tokens, error) {
	if s.accessToken != "" && s.refreshToken != "" {
		return &Tokens{AccessToken: s.accessToken, RefreshToken: s.refreshToken, ExpiresAt: time.Now().UTC().Add(365 * 24 * time.Hour), TokenType: "bearer"}, nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tokens Tokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}

func (s *FileStorage) Clear(_ context.Context) error {
	if err := os.Remove(s.path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else {
		return err
	}
}

func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func openURL(value string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", value).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", value).Start()
	default:
		return exec.Command("xdg-open", value).Start()
	}
}
