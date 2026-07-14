package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/logsafe"
)

const sessionCookieName = "pjsk_admin_session"

var dummyPasswordHash, _ = bcrypt.GenerateFromPassword(
	[]byte("invalid-login-placeholder"),
	bcrypt.DefaultCost,
)

type Handler struct {
	store        Store
	sessionTTL   time.Duration
	cookieSecure bool
	now          func() time.Time
	random       io.Reader
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type adminResponse struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name,omitempty"`
	Role        string  `json:"role"`
}

type authenticationResponse struct {
	Admin adminResponse `json:"admin"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandler(store Store, sessionTTL time.Duration, cookieSecure bool) *Handler {
	return &Handler{
		store:        store,
		sessionTTL:   sessionTTL,
		cookieSecure: cookieSecure,
		now:          time.Now,
		random:       rand.Reader,
	}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var request loginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	username := strings.TrimSpace(request.Username)
	if username == "" || request.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	account, err := h.store.FindByUsername(r.Context(), username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		log.Printf("find admin for login: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	passwordHash := []byte(account.PasswordHash)
	if errors.Is(err, ErrNotFound) {
		passwordHash = dummyPasswordHash
	}
	passwordMatches := bcrypt.CompareHashAndPassword(
		passwordHash,
		[]byte(request.Password),
	) == nil
	if errors.Is(err, ErrNotFound) || account.Status != "active" || !passwordMatches {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token, tokenHash, err := newSessionToken(h.random)
	if err != nil {
		log.Printf("generate admin session token: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	expiresAt := h.now().Add(h.sessionTTL)
	if err := h.store.CreateSession(r.Context(), account.ID, tokenHash, expiresAt); err != nil {
		log.Printf("create admin session: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.setSessionCookie(w, token, expiresAt)
	writeJSON(w, http.StatusOK, authenticationResponse{Admin: responseFromAdmin(account)})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	account, ok := CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	writeJSON(w, http.StatusOK, authenticationResponse{Admin: responseFromAdmin(account)})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	tokenHash, ok := sessionHashFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := h.store.DeleteSession(r.Context(), tokenHash); err != nil {
		log.Printf("delete admin session: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func newSessionToken(random io.Reader) (string, string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(value)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func sessionHashFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return hashToken(cookie.Value), true
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(h.sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func responseFromAdmin(account Admin) adminResponse {
	return adminResponse{
		ID:          account.ID,
		Username:    account.Username,
		DisplayName: account.DisplayName,
		Role:        account.Role,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode admin JSON response: %v", err)
	}
}
