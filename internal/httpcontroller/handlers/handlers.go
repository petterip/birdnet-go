package handlers

import (
	"encoding/json"
	"errors"
	"time"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"

	"github.com/labstack/echo/v4"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/imageprovider"
	"github.com/tphakala/birdnet-go/internal/myaudio"
	"github.com/tphakala/birdnet-go/internal/suncalc"
    "github.com/tphakala/birdnet-go/internal/oauth2"
)



// Handlers embeds baseHandler and includes all the dependencies needed for the application handlers.
type Handlers struct {
	baseHandler
	DS                datastore.Interface
	Settings          *conf.Settings
	DashboardSettings *conf.Dashboard
	BirdImageCache    *imageprovider.BirdImageCache
	SSE               *SSEHandler                 // Server Side Events handler
	SunCalc           *suncalc.SunCalc            // SunCalc instance for calculating sun event times
	AudioLevelChan    chan myaudio.AudioLevelData // Channel for audio level updates
    OAuth2Server      *oauth2.OAuth2Server
}

// HandlerError is a custom error type that includes an HTTP status code and a user-friendly message.
type HandlerError struct {
	Err     error
	Message string
	Code    int
}

// Error implements the error interface for HandlerError.
func (e *HandlerError) Error() string {
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

// baseHandler provides common functionality for all handlers.
type baseHandler struct {
	errorHandler func(error) *HandlerError
	logger       *log.Logger
}

// newHandlerError creates a new HandlerError with the given parameters.
func (bh *baseHandler) NewHandlerError(err error, message string, code int) *HandlerError {
	handlerErr := &HandlerError{
		Err:     err,
		Message: fmt.Sprintf("%s: %v", message, err),
		Code:    code,
	}
	bh.logError(handlerErr)
	return handlerErr
}

// logError logs an error message.
func (bh *baseHandler) logError(err *HandlerError) {
	bh.logger.Printf("Error: %s (Code: %d, Underlying error: %v)", err.Message, err.Code, err.Err)
}

// logInfo logs an info message.
func (bh *baseHandler) logInfo(message string) {
	bh.logger.Printf("Info: %s", message)
}

// New creates a new Handlers instance with the given dependencies.
func New(ds datastore.Interface, settings *conf.Settings, dashboardSettings *conf.Dashboard, birdImageCache *imageprovider.BirdImageCache, logger *log.Logger, sunCalc *suncalc.SunCalc, audioLevelChan chan myaudio.AudioLevelData) *Handlers {
	if logger == nil {
		logger = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	}

    oauth2Server := oauth2.NewOAuth2Server(settings)

	return &Handlers{
		baseHandler: baseHandler{
			errorHandler: defaultErrorHandler,
			logger:       logger,
		},
		DS:                ds,
		Settings:          settings,
		DashboardSettings: dashboardSettings,
		BirdImageCache:    birdImageCache,
		SSE:               NewSSEHandler(),
		SunCalc:           sunCalc,
		AudioLevelChan:    audioLevelChan,
        OAuth2Server:      oauth2Server,
	}
}

// defaultErrorHandler is the default implementation of error handling.
func defaultErrorHandler(err error) *HandlerError {
	var he *HandlerError
	if errors.As(err, &he) {
		return he
	}
	return &HandlerError{
		Err:     err,
		Message: "An unexpected error occurred",
		Code:    http.StatusInternalServerError,
	}
}

// HandleError is a utility method to handle errors in Echo handlers.
func (h *Handlers) HandleError(err error, c echo.Context) error {
	var he *HandlerError
	var echoHTTPError *echo.HTTPError

	// Check if it's an Echo HTTP error
	if errors.As(err, &echoHTTPError) {
		he = &HandlerError{
			Err:     echoHTTPError,
			Message: fmt.Sprintf("%v", echoHTTPError.Message),
			Code:    echoHTTPError.Code,
		}
	} else if errors.As(err, &he) {
		// It's already a HandlerError, use it as is
	} else {
		// For any other error, treat it as an internal server error
		he = &HandlerError{
			Err:     err,
			Message: "An unexpected error occurred",
			Code:    http.StatusInternalServerError,
		}
	}

	// Check if headers have already been sent
	if c.Response().Committed {
		return nil
	}

	errorData := struct {
		Title        string
		Message      string
		Debug        bool
		ErrorDetails string
		StackTrace   string
	}{
		Title:   fmt.Sprintf("%d Error", he.Code),
		Message: he.Message,
		Debug:   h.Settings.Debug,
	}

	// Include error details and stack trace
	// TODO: this should be hidden for clients outside of server subnet
	errorData.ErrorDetails = fmt.Sprintf("%v", he.Err)
	errorData.StackTrace = string(debug.Stack())

	// Choose the appropriate template based on the error code
	template := h.getErrorTemplate(he.Code)

	// Set the response status code to the error code
	c.Response().Status = he.Code

	// Render the template with the correct status code
	return c.Render(he.Code, template, errorData)
}

// getErrorTemplate returns the appropriate error template name based on the error code
func (h *Handlers) getErrorTemplate(code int) string {
	switch code {
	case http.StatusNotFound:
		return "error-404"
	case http.StatusInternalServerError:
		return "error-500"
	default:
		return "error" // fallback to the generic error template
	}
}

// WithErrorHandling wraps an Echo handler function with error handling.
func (h *Handlers) WithErrorHandling(fn func(echo.Context) error) echo.HandlerFunc {
	return func(c echo.Context) error {
		err := fn(c)
		if err != nil {
			return h.HandleError(err, c)
		}
		return nil
	}
}

// AudioLevelSSE handles Server-Sent Events for real-time audio level updates
func (h *Handlers) AudioLevelSSE(c echo.Context) error {
	// Set headers for SSE
	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
	c.Response().Header().Set(echo.HeaderConnection, "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	for {
		select {
		case <-c.Request().Context().Done():
			// Client disconnected
			return nil
		case audioData := <-h.AudioLevelChan:
			// Prepare data structure for JSON encoding
			data := struct {
				Level    int  `json:"level"`
				Clipping bool `json:"clipping"`
			}{
				Level:    audioData.Level,
				Clipping: audioData.Clipping,
			}

			// Marshal data to JSON
			jsonData, err := json.Marshal(data)
			if err != nil {
				return err
			}

			// Write SSE formatted data
			if _, err := c.Response().Write([]byte(fmt.Sprintf("data: %s\n\n", jsonData))); err != nil {
				return err
			}

			// Flush the response writer buffer
			c.Response().Flush()
		}
	}
}

func (h *Handlers) HandleAuthorize(c echo.Context) error {
    clientID := c.QueryParam("client_id")
    redirectURI := c.QueryParam("redirect_uri")
    
    if clientID != h.Settings.OAuth2.ClientID || redirectURI != h.Settings.OAuth2.RedirectURI {
        return c.String(http.StatusBadRequest, "Invalid client_id or redirect_uri")
    }

    // In a real-world scenario, you'd typically show a login page here
    // For simplicity, we're auto-generating an auth code
    authCode, err := h.OAuth2Server.GenerateAuthCode()
    if err != nil {
        return c.String(http.StatusInternalServerError, "Error generating auth code")
    }

    return c.Redirect(http.StatusFound, redirectURI + "?code=" + authCode)
}

func (h *Handlers) HandleToken(c echo.Context) error {
    grantType := c.FormValue("grant_type")
    code := c.FormValue("code")
    clientID := c.FormValue("client_id")
    clientSecret := c.FormValue("client_secret")

    if grantType != "authorization_code" || clientID != h.Settings.OAuth2.ClientID || clientSecret != h.Settings.OAuth2.ClientSecret {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_request"})
    }

    accessToken, err := h.OAuth2Server.ExchangeAuthCode(code)
    if err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
    }

    return c.JSON(http.StatusOK, map[string]string{
        "access_token": accessToken,
        "token_type": "Bearer",
        "expires_in": h.Settings.OAuth2.AccessTokenExp.String(),
    })
}

func (h *Handlers) HandleOAuth2Callback(c echo.Context) error {
    code := c.QueryParam("code")
    if code == "" {
        return c.String(http.StatusBadRequest, "Missing authorization code")
    }

    // Exchange the authorization code for an access token
    accessToken, err := h.OAuth2Server.ExchangeAuthCode(code)
    if err != nil {
        return c.String(http.StatusInternalServerError, "Failed to exchange authorization code")
    }

    // Set the access token in a secure HTTP-only cookie
    cookie := new(http.Cookie)
    cookie.Name = "access_token"
    cookie.Value = accessToken
    cookie.Expires = time.Now().Add(h.Settings.OAuth2.AccessTokenExp)
    cookie.Path = "/"
    cookie.HttpOnly = true
    cookie.Secure = true // Set to true if using HTTPS
    c.SetCookie(cookie)

    // Redirect to the settings page
    return c.Redirect(http.StatusFound, "/settings/main")
}