package oauth2

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/tphakala/birdnet-go/internal/conf"
)

type AuthCode struct {
    Code      string
    ExpiresAt time.Time
}

type AccessToken struct {
    Token     string
    ExpiresAt time.Time
}

type OAuth2Server struct {
    config       *conf.Settings
    authCodes    map[string]AuthCode
    accessTokens map[string]AccessToken
    mutex        sync.RWMutex
}

func NewOAuth2Server(config *conf.Settings) *OAuth2Server {
    return &OAuth2Server{
        config:       config,
        authCodes:    make(map[string]AuthCode),
        accessTokens: make(map[string]AccessToken),
    }
}

func (s *OAuth2Server) GenerateAuthCode() (string, error) {
    code := make([]byte, 32)
    _, err := rand.Read(code)
    if err != nil {
        return "", err
    }
    authCode := base64.URLEncoding.EncodeToString(code)
    
    s.mutex.Lock()
    defer s.mutex.Unlock()
    
    s.authCodes[authCode] = AuthCode{
        Code:      authCode,
        ExpiresAt: time.Now().Add(s.config.OAuth2.AuthCodeExp),
    }
    return authCode, nil
}

func (s *OAuth2Server) ExchangeAuthCode(code string) (string, error) {
    s.mutex.Lock()
    defer s.mutex.Unlock()

    authCode, exists := s.authCodes[code]
    if !exists || time.Now().After(authCode.ExpiresAt) {
        return "", errors.New("invalid or expired auth code")
    }
    delete(s.authCodes, code)

    token := make([]byte, 32)
    _, err := rand.Read(token)
    if err != nil {
        return "", err
    }
    accessToken := base64.URLEncoding.EncodeToString(token)
    s.accessTokens[accessToken] = AccessToken{
        Token:     accessToken,
        ExpiresAt: time.Now().Add(s.config.OAuth2.AccessTokenExp),
    }
    return accessToken, nil
}

func (s *OAuth2Server) ValidateAccessToken(token string) bool {
    s.mutex.RLock()
    defer s.mutex.RUnlock()
    
    accessToken, exists := s.accessTokens[token]
    if !exists {
        return false
    }
    
    return time.Now().Before(accessToken.ExpiresAt)
}
