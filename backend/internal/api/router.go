package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"pjsk/backend/internal/admin"
	"pjsk/backend/internal/clientip"
	"pjsk/backend/internal/config"
	"pjsk/backend/internal/export"
	"pjsk/backend/internal/importpreview"
	"pjsk/backend/internal/orders"
	"pjsk/backend/internal/paymentqr"
	"pjsk/backend/internal/payments"
	"pjsk/backend/internal/paymentsubmission"
	"pjsk/backend/internal/query"
	"pjsk/backend/internal/querycoderecovery"
	"pjsk/backend/internal/recoveryemail"
	"pjsk/backend/internal/recoveryemailverification"
	"pjsk/backend/internal/users"

	"github.com/jackc/pgx/v5/pgxpool"
)

type server struct {
	config config.Config
	dbPool *pgxpool.Pool
}

type healthResponse struct {
	Service  string `json:"service"`
	Status   string `json:"status"`
	Database string `json:"database"`
	Time     string `json:"time"`
}

type appConfigResponse struct {
	Name                 string   `json:"name"`
	Stage                string   `json:"stage"`
	LegacyAdminPort      string   `json:"legacyAdminPort"`
	LegacyUserPort       string   `json:"legacyUserPort"`
	FrontendOrigins      []string `json:"frontendOrigins"`
	EmailDeliveryEnabled bool     `json:"emailDeliveryEnabled"`
	Modules              []module `json:"modules"`
}

type module struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

func NewRouter(cfg config.Config, dbPool *pgxpool.Pool) http.Handler {
	server := &server{
		config: cfg,
		dbPool: dbPool,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.healthHandler)
	mux.HandleFunc("/api/config", server.configHandler)

	clientIPResolver := clientip.NewResolver(cfg.TrustedProxyCIDRs)
	resolveClientIP := func(r *http.Request) string {
		return clientIPResolver.Resolve(r).Key()
	}

	adminStore := admin.NewPostgresStore(dbPool)
	adminHandler := admin.NewHandler(
		adminStore,
		cfg.AdminSessionTTL,
		cfg.CookieSecure,
	)
	adminHandler.ConfigureClientIPResolver(resolveClientIP)
	mux.HandleFunc("/api/admin/login", adminHandler.Login)
	mux.Handle("/api/admin/me", adminHandler.RequireAuthentication(http.HandlerFunc(adminHandler.Me)))
	mux.Handle("/api/admin/logout", adminHandler.RequireAuthentication(http.HandlerFunc(adminHandler.Logout)))

	// Owner/security endpoints. Recovery-code generation is owner-only and,
	// like recovery-email binding, requires a fresh re-authentication. The
	// three /api/admin/recovery/* endpoints are deliberately unauthenticated
	// single-step reset flows: they never issue a session.
	mux.Handle("/api/admin/reauth", adminHandler.RequireAuthentication(http.HandlerFunc(adminHandler.Reauth)))
	mux.Handle("/api/admin/security/password", adminHandler.RequireAuthentication(http.HandlerFunc(adminHandler.ChangePassword)))
	mux.Handle("/api/admin/security/recovery-email", adminHandler.RequireAuthentication(http.HandlerFunc(adminHandler.RecoveryEmailStatus)))
	mux.Handle("/api/admin/security/recovery-email/request", adminHandler.RequireAuthentication(adminHandler.RequireRecentReauth(http.HandlerFunc(adminHandler.RecoveryEmailBindRequest))))
	mux.Handle("/api/admin/security/recovery-email/confirm", adminHandler.RequireAuthentication(http.HandlerFunc(adminHandler.RecoveryEmailBindConfirm)))
	mux.Handle("/api/admin/security/audit-summary", adminHandler.RequireAuthentication(http.HandlerFunc(adminHandler.AuditSummary)))
	mux.Handle("/api/admin/owner/recovery-codes", adminHandler.RequireAuthentication(adminHandler.RequireOwner(adminHandler.RequireRecentReauthWhen(admin.MutatingMatch, http.HandlerFunc(adminHandler.OwnerRecoveryCodes)))))
	mux.HandleFunc("/api/admin/recovery/code-reset", adminHandler.RecoveryCodeReset)
	mux.HandleFunc("/api/admin/recovery/email-request", adminHandler.RecoveryEmailResetRequest)
	mux.HandleFunc("/api/admin/recovery/email-reset", adminHandler.RecoveryEmailReset)

	importPreviewHandler := importpreview.NewHandler(importpreview.NewPostgresStore(dbPool))
	mux.Handle(
		"/api/admin/imports/preview",
		adminHandler.RequireAuthentication(http.HandlerFunc(importPreviewHandler.Preview)),
	)
	mux.Handle(
		"/api/admin/imports/confirm",
		adminHandler.RequireAuthentication(http.HandlerFunc(importPreviewHandler.Confirm)),
	)
	// Exact pattern, registered before the "/api/admin/imports/" prefix so it
	// is not read as an import id by the detail handler.
	mux.Handle(
		"/api/admin/imports/facets",
		adminHandler.RequireAuthentication(http.HandlerFunc(importPreviewHandler.Facets)),
	)
	mux.Handle(
		"/api/admin/imports",
		adminHandler.RequireAuthentication(http.HandlerFunc(importPreviewHandler.List)),
	)
	mux.Handle(
		"/api/admin/imports/",
		// Import revert is high-risk: mutating …/revert requests additionally
		// require a fresh re-authentication; detail reads stay ungated.
		adminHandler.RequireAuthentication(adminHandler.RequireRecentReauthWhen(
			admin.MutatingSuffixMatch("/revert"),
			http.HandlerFunc(importPreviewHandler.Detail),
		)),
	)

	ordersHandler := orders.NewHandler(orders.NewPostgresStore(dbPool))
	mux.Handle(
		"/api/admin/orders",
		adminHandler.RequireAuthentication(http.HandlerFunc(ordersHandler.List)),
	)
	// Registered before the "/api/admin/orders/" prefix so the facets endpoint
	// is not read as an order id. ServeMux prefers the longer exact pattern.
	mux.Handle(
		"/api/admin/orders/facets",
		adminHandler.RequireAuthentication(http.HandlerFunc(ordersHandler.Facets)),
	)
	mux.Handle(
		"/api/admin/orders/",
		adminHandler.RequireAuthentication(http.HandlerFunc(ordersHandler.Detail)),
	)

	usersStore := users.NewPostgresStore(dbPool)
	usersHandler := users.NewHandler(usersStore)
	recoveryEmailProtector, _ := recoveryemail.NewProtector(
		cfg.RecoveryEmailEncryptionKey,
		cfg.RecoveryEmailHMACKey,
	)
	usersHandler.ConfigureRecoveryEmail(usersStore, recoveryEmailProtector)
	mux.Handle(
		"/api/admin/users",
		adminHandler.RequireAuthentication(http.HandlerFunc(usersHandler.List)),
	)
	// Registered as an exact pattern so it is not read as a user id by the
	// "/api/admin/users/" prefix handler. ServeMux prefers the longer exact
	// pattern.
	mux.Handle(
		"/api/admin/users/facets",
		adminHandler.RequireAuthentication(http.HandlerFunc(usersHandler.Facets)),
	)
	mux.Handle(
		"/api/admin/users/bind-token-batch-preview",
		adminHandler.RequireAuthentication(http.HandlerFunc(usersHandler.BulkBindTokenPreview)),
	)
	mux.Handle(
		"/api/admin/users/merge-preview",
		adminHandler.RequireAuthentication(http.HandlerFunc(usersHandler.MergePreview)),
	)
	mux.Handle(
		"/api/admin/users/merge",
		// User merge is high-risk and always requires a fresh re-authentication.
		adminHandler.RequireAuthentication(adminHandler.RequireRecentReauth(http.HandlerFunc(usersHandler.Merge))),
	)
	mux.Handle(
		"/api/admin/users/",
		adminHandler.RequireAuthentication(http.HandlerFunc(usersHandler.Detail)),
	)

	paymentsHandler := payments.NewHandler(payments.NewPostgresStore(dbPool))
	mux.Handle(
		// Registered as an exact pattern so it is not read as a payment id by
		// the "/api/admin/payments/" prefix handler. ServeMux prefers the
		// longer exact pattern.
		"/api/admin/payments/facets",
		adminHandler.RequireAuthentication(http.HandlerFunc(paymentsHandler.Facets)),
	)
	mux.Handle(
		"/api/admin/payments/cn",
		adminHandler.RequireAuthentication(http.HandlerFunc(paymentsHandler.CN)),
	)
	mux.Handle(
		"/api/admin/payments/unpaid",
		adminHandler.RequireAuthentication(http.HandlerFunc(paymentsHandler.Unpaid)),
	)
	mux.Handle(
		"/api/admin/payments/",
		// Payment void is high-risk: mutating …/void requests additionally
		// require a fresh re-authentication; detail reads stay ungated.
		adminHandler.RequireAuthentication(adminHandler.RequireRecentReauthWhen(
			admin.MutatingSuffixMatch("/void"),
			http.HandlerFunc(paymentsHandler.Detail),
		)),
	)
	mux.Handle(
		"/api/admin/payments",
		adminHandler.RequireAuthentication(http.HandlerFunc(paymentsHandler.Collection)),
	)

	exportHandler := export.NewHandler(
		users.NewPostgresStore(dbPool),
		payments.NewPostgresStore(dbPool),
		dbPool,
	)
	mux.Handle(
		"/api/admin/export/users.csv",
		adminHandler.RequireAuthentication(http.HandlerFunc(exportHandler.Users)),
	)
	mux.Handle(
		"/api/admin/export/users.xlsx",
		adminHandler.RequireAuthentication(http.HandlerFunc(exportHandler.UsersExcel)),
	)
	mux.Handle(
		// The only export that mutates: it issues a live bind code per user in
		// the filter result. Always requires a fresh re-authentication.
		"/api/admin/export/bind-tokens.xlsx",
		adminHandler.RequireAuthentication(adminHandler.RequireRecentReauth(http.HandlerFunc(exportHandler.BindTokensExcel))),
	)
	mux.Handle(
		"/api/admin/export/payments.csv",
		adminHandler.RequireAuthentication(http.HandlerFunc(exportHandler.Payments)),
	)
	mux.Handle(
		"/api/admin/export/payments.xlsx",
		adminHandler.RequireAuthentication(http.HandlerFunc(exportHandler.PaymentsExcel)),
	)
	mux.Handle(
		"/api/admin/export/order-items.csv",
		adminHandler.RequireAuthentication(http.HandlerFunc(exportHandler.OrderItems)),
	)
	mux.Handle(
		"/api/admin/export/order-items.xlsx",
		adminHandler.RequireAuthentication(http.HandlerFunc(exportHandler.OrderItemsExcel)),
	)

	queryHandler := query.NewHandler(
		query.NewPostgresStore(dbPool),
		cfg.AdminSessionTTL,
		cfg.CookieSecure,
	)
	queryHandler.ConfigureClientIPResolver(resolveClientIP)
	queryHandler.ConfigureRecoveryEmail(usersStore, recoveryEmailProtector)
	// An admin resetting a user's query code must also clear that user's
	// login-side block, otherwise the user still cannot log in with the code
	// they were just given.
	usersHandler.ConfigureLoginLockReleaser(queryHandler)
	if verificationManager, err := recoveryemailverification.NewManager(cfg.RecoveryEmailVerificationHMACKey); err == nil && recoveryEmailProtector != nil {
		var sender recoveryemailverification.Sender
		switch cfg.RecoveryEmailSenderMode {
		case "fake":
			sender = recoveryemailverification.NewFakeSender()
		case "smtp":
			sender, _ = recoveryemailverification.NewSMTPSender(recoveryemailverification.SMTPConfig{
				Host: cfg.RecoveryEmailSMTP.Host, Port: cfg.RecoveryEmailSMTP.Port,
				Username: cfg.RecoveryEmailSMTP.Username, Password: cfg.RecoveryEmailSMTP.Password,
				From: cfg.RecoveryEmailSMTP.From, FromName: cfg.RecoveryEmailSMTP.FromName,
				TLSMode: cfg.RecoveryEmailSMTP.TLSMode,
			})
		}
		verificationStore := recoveryemailverification.NewPostgresStore(dbPool, verificationManager)
		queryHandler.ConfigureRecoveryEmailVerification(
			recoveryemailverification.NewService(verificationStore, recoveryEmailProtector, sender, verificationManager.Policy()),
		)
		if recoveryManager, recoveryErr := querycoderecovery.NewManager(cfg.QueryCodeRecoveryHMACKey); recoveryErr == nil {
			if recoverySender, ok := sender.(querycoderecovery.Sender); ok {
				recoveryStore := querycoderecovery.NewPostgresStore(dbPool, recoveryManager)
				queryHandler.ConfigureQueryCodeRecovery(
					querycoderecovery.NewService(recoveryStore, recoveryEmailProtector, recoverySender, recoveryManager),
				)
			}
		}
	}
	// Owner/security capabilities for the admin handler. Email delivery
	// reuses the user recovery sender mode; with mode "disabled" the sender
	// stays nil and every admin email-recovery endpoint answers an explicit
	// 503 instead of pretending to send. The admin protector uses its own
	// AAD so admin and user recovery email ciphertexts stay domain-separated.
	var adminEmailSender recoveryemailverification.Sender
	switch cfg.RecoveryEmailSenderMode {
	case "fake":
		adminEmailSender = recoveryemailverification.NewFakeSender()
	case "smtp":
		adminEmailSender, _ = recoveryemailverification.NewSMTPSender(recoveryemailverification.SMTPConfig{
			Host: cfg.RecoveryEmailSMTP.Host, Port: cfg.RecoveryEmailSMTP.Port,
			Username: cfg.RecoveryEmailSMTP.Username, Password: cfg.RecoveryEmailSMTP.Password,
			From: cfg.RecoveryEmailSMTP.From, FromName: cfg.RecoveryEmailSMTP.FromName,
			TLSMode: cfg.RecoveryEmailSMTP.TLSMode,
		})
	}
	adminRecoveryEmailProtector, _ := recoveryemail.NewProtectorWithAAD(
		cfg.RecoveryEmailEncryptionKey,
		cfg.RecoveryEmailHMACKey,
		admin.AdminRecoveryEmailAAD,
	)
	adminHandler.ConfigureSecurity(adminStore, cfg.AdminRecoveryCodeHMACKey, adminRecoveryEmailProtector, adminEmailSender)
	adminHandler.ConfigureManagement(adminStore)

	// Owner-only admin management. Every route requires an authenticated
	// owner; mutations (appoint/enable/disable/revoke/reset-password)
	// additionally require a fresh re-authentication. The storage layer
	// re-checks that the target is never the owner, so no frontend state can
	// bypass the wall.
	mux.Handle(
		"/api/admin/owner/admins",
		adminHandler.RequireAuthentication(adminHandler.RequireOwner(adminHandler.RequireRecentReauthWhen(
			admin.MutatingMatch,
			http.HandlerFunc(adminHandler.ManagementCollection),
		))),
	)
	mux.Handle(
		"/api/admin/owner/admins/",
		adminHandler.RequireAuthentication(adminHandler.RequireOwner(adminHandler.RequireRecentReauthWhen(
			admin.MutatingMatch,
			http.HandlerFunc(adminHandler.ManagementItem),
		))),
	)

	mux.HandleFunc("/api/query/login", queryHandler.Login)
	mux.HandleFunc("/api/query/change-code", queryHandler.ChangeCode)
	mux.HandleFunc("/api/query/bind-code", queryHandler.BindCode)
	mux.HandleFunc("/api/query/recovery-email", queryHandler.RecoveryEmail)
	mux.HandleFunc("/api/query/recovery-email/send-verification", queryHandler.SendRecoveryEmailVerification)
	mux.HandleFunc("/api/query/recovery-email/verify", queryHandler.VerifyRecoveryEmail)
	mux.HandleFunc("/api/query/recovery/request", queryHandler.RequestQueryCodeRecovery)
	mux.HandleFunc("/api/query/recovery/verify", queryHandler.VerifyQueryCodeRecovery)
	mux.HandleFunc("/api/query/recovery/reset", queryHandler.ResetRecoveredQueryCode)
	mux.HandleFunc("/api/query/orders", queryHandler.Orders)
	mux.HandleFunc("/api/query/logout", queryHandler.Logout)

	// Payment collection QR codes. Admin routes manage the codes (auth: admin
	// session); user routes read the currently enabled code (auth: query
	// session). QR images are never exposed to unauthenticated requests.
	qrHandler := paymentqr.NewHandler(paymentqr.NewPostgresStore(dbPool))
	mux.Handle(
		"/api/admin/payment-qr",
		// QR changes are high-risk: mutating requests additionally require a
		// fresh re-authentication; status reads stay ungated.
		adminHandler.RequireAuthentication(adminHandler.RequireRecentReauthWhen(
			admin.MutatingMatch,
			http.HandlerFunc(qrHandler.AdminCollection),
		)),
	)
	mux.Handle(
		"/api/admin/payment-qr/",
		adminHandler.RequireAuthentication(adminHandler.RequireRecentReauthWhen(
			admin.MutatingMatch,
			http.HandlerFunc(qrHandler.AdminItem),
		)),
	)
	mux.Handle(
		"/api/query/payment-qr",
		queryHandler.RequireSession(http.HandlerFunc(qrHandler.UserAvailability)),
	)
	mux.Handle(
		"/api/query/payment-qr/",
		queryHandler.RequireSession(http.HandlerFunc(qrHandler.UserImage)),
	)

	// Payment proof ("收肾记录"). A submission is evidence only and never moves a
	// paid total by itself; an admin approval creates a real approved payment via
	// the shared payments transaction core. User routes carry the injected
	// session identity (RequireSessionUser); admin routes use the admin session.
	submissionHandler := paymentsubmission.NewHandler(
		paymentsubmission.NewPostgresStore(dbPool, payments.NewPostgresStore(dbPool)),
	)
	mux.Handle(
		"/api/query/payment-submissions",
		queryHandler.RequireSessionUser(http.HandlerFunc(submissionHandler.UserCollection)),
	)
	mux.Handle(
		"/api/query/payment-submissions/",
		queryHandler.RequireSessionUser(http.HandlerFunc(submissionHandler.UserImage)),
	)
	// Exact facets pattern registered before the "/{id}" prefix so ServeMux
	// prefers it and it is never read as a submission id.
	mux.Handle(
		"/api/admin/payment-submissions/facets",
		adminHandler.RequireAuthentication(http.HandlerFunc(submissionHandler.Facets)),
	)
	mux.Handle(
		"/api/admin/payment-submissions",
		adminHandler.RequireAuthentication(http.HandlerFunc(submissionHandler.AdminCollection)),
	)
	mux.Handle(
		"/api/admin/payment-submissions/",
		adminHandler.RequireAuthentication(http.HandlerFunc(submissionHandler.AdminItem)),
	)

	return withCORS(loggingMiddleware(mux), cfg.FrontendOrigins)
}

func (s *server) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	databaseStatus := "connected"
	status := "ok"
	httpStatus := http.StatusOK

	if s.dbPool == nil {
		databaseStatus = "not_initialized"
		status = "error"
		httpStatus = http.StatusServiceUnavailable
	} else if err := s.dbPool.Ping(ctx); err != nil {
		databaseStatus = "disconnected"
		status = "error"
		httpStatus = http.StatusServiceUnavailable
	}

	writeJSON(w, httpStatus, healthResponse{
		Service:  "pjsk-backend",
		Status:   status,
		Database: databaseStatus,
		Time:     time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *server) configHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	configResponse := appConfigResponse{
		Name:                 "PJSK Goods Next",
		Stage:                "foundation",
		LegacyAdminPort:      s.config.LegacyAdminPort,
		LegacyUserPort:       s.config.LegacyUserPort,
		FrontendOrigins:      s.config.FrontendOrigins,
		EmailDeliveryEnabled: s.config.RecoveryEmailSenderMode == "smtp" || s.config.RecoveryEmailSenderMode == "fake",
		Modules: []module{
			{
				Key:         "frontend-shell",
				Title:       "Frontend shell",
				Status:      "ready",
				Description: "Vue 3 shell is online and can read backend status.",
			},
			{
				Key:         "backend-core",
				Title:       "Backend core",
				Status:      "ready",
				Description: "Go server is connected to PostgreSQL and runs migrations on startup.",
			},
			{
				Key:         "database-core",
				Title:       "Database core",
				Status:      "ready",
				Description: "Core project, order, and payment tables are available.",
			},
			{
				Key:         "excel-import",
				Title:       "Excel import",
				Status:      "ready",
				Description: "Admins can preview, confirm, list, and revert Excel imports.",
			},
			{
				Key:         "payment-workflow",
				Title:       "Payment workflow",
				Status:      "ready",
				Description: "Admins review unpaid CN balances and record payments manually, with partial payments, WeChat fee calculation, payment detail, and void with audit trail.",
			},
		},
	}

	writeJSON(w, http.StatusOK, configResponse)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf(
			"%s %s %s",
			r.Method,
			r.URL.Path,
			time.Since(start).Round(time.Millisecond),
		)
	})
}

// corsAllowedMethods and corsAllowedHeaders mirror what the frontend
// actually sends (fetch with GET/POST/PATCH/PUT/DELETE and a JSON
// Content-Type); they are deliberately not wide open.
const (
	corsAllowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	corsAllowedHeaders = "Content-Type"
)

func withCORS(next http.Handler, allowedOrigins []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Same-origin or non-browser request: no CORS headers at all.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		appendVaryOrigin(w.Header())

		if isAllowedOrigin(origin, allowedOrigins) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", corsAllowedMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// Disallowed origin: no allow headers; the browser blocks the
		// cross-origin read, and preflights fail for lack of an ACAO header.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// appendVaryOrigin adds Origin to the Vary header without clobbering or
// duplicating existing values.
func appendVaryOrigin(header http.Header) {
	for _, value := range header.Values("Vary") {
		for _, member := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(member), "Origin") {
				return
			}
		}
	}
	header.Add("Vary", "Origin")
}

func isAllowedOrigin(origin string, allowedOrigins []string) bool {
	for _, item := range allowedOrigins {
		if strings.EqualFold(origin, item) {
			return true
		}
	}

	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode JSON response: %v", err)
	}
}
