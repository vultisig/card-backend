package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/reapmapping"
	"github.com/vultisig/card-backend/internal/service"
	"github.com/vultisig/card-backend/internal/statsd"
)

type Server struct {
	echo                   *echo.Echo
	pool                   *pgxpool.Pool
	stats                  *statsd.Client
	authService            *service.AuthService
	userService            *service.UserService
	accountService         *service.AccountService
	cardService            *service.CardService
	cardShipmentService    *service.CardShipmentService
	cardDesignService      *service.CardDesignService
	cardTransactionService *service.CardTransactionService
	activityService        *service.ActivityService
	fraudAlertService      *service.FraudAlertService
	simulationService      *service.SimulationService
	webhookService         *service.WebhookService
}

func NewServer(pool *pgxpool.Pool, stats *statsd.Client, authService *service.AuthService, userService *service.UserService, accountService *service.AccountService, cardService *service.CardService, cardShipmentService *service.CardShipmentService, cardDesignService *service.CardDesignService, cardTransactionService *service.CardTransactionService, activityService *service.ActivityService, fraudAlertService *service.FraudAlertService, simulationService *service.SimulationService, webhookService *service.WebhookService, reapEnv string) *Server {
	s := &Server{
		echo:                   echo.New(),
		pool:                   pool,
		stats:                  stats,
		authService:            authService,
		userService:            userService,
		accountService:         accountService,
		cardService:            cardService,
		cardShipmentService:    cardShipmentService,
		cardDesignService:      cardDesignService,
		cardTransactionService: cardTransactionService,
		activityService:        activityService,
		fraudAlertService:      fraudAlertService,
		simulationService:      simulationService,
		webhookService:         webhookService,
	}

	s.echo.Use(middleware.Recover())
	s.echo.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:    true,
		LogURIPath:   true,
		LogMethod:    true,
		LogRoutePath: true,
		LogLatency:   true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			// URIPath, not URI: URI is req.RequestURI verbatim, which
			// includes the query string — logging it risks leaking
			// anything passed as a query param (e.g. a future token).
			log.Printf("%s %s %d", v.Method, v.URIPath, v.Status)
			// RoutePath (e.g. "/card/:id"), not URI, keeps tag cardinality
			// bounded — URI would mint a new tag value per card/account ID.
			route := v.RoutePath
			if route == "" {
				route = "unmatched"
			}
			tags := []string{"method:" + v.Method, "route:" + route, "status:" + strconv.Itoa(v.Status)}
			s.stats.Count("http.request.count", 1, tags...)
			s.stats.Timing("http.request.duration", v.Latency, tags...)
			return nil
		},
	}))
	s.echo.Use(middleware.RequestID())
	s.echo.Use(middleware.CORS())
	s.echo.Use(middleware.Secure())
	s.echo.Use(middleware.Gzip())
	s.echo.Use(middleware.BodyLimit("1M"))

	s.echo.GET("/health", s.health)
	s.echo.GET("/nonce", s.nonce)
	s.echo.POST("/auth", s.auth)
	s.echo.POST("/webhooks/reap", s.reapWebhook)

	userGroup := s.echo.Group("/user", s.authService.RequireAuth)
	userGroup.POST("", s.createUser)
	userGroup.GET("", s.getUser)
	userGroup.PUT("/email", s.updateUserEmail)
	userGroup.PUT("/phone", s.updateUserPhone)
	userGroup.POST("/application", s.advanceUserApplication)

	accountGroup := s.echo.Group("/account", s.authService.RequireAuth)
	accountGroup.POST("", s.createAccount)
	accountGroup.GET("", s.listAccounts)
	accountGroup.GET("/signer-message", s.generateSignerMessage)
	accountGroup.GET("/:id", s.getAccount)
	accountGroup.GET("/:id/balance", s.getAccountBalance)
	accountGroup.GET("/:id/assets", s.getAccountAssets)

	cardGroup := s.echo.Group("/card", s.authService.RequireAuth)
	cardGroup.POST("", s.createCard)
	cardGroup.GET("", s.listCards)
	cardGroup.GET("/:id", s.getCard)
	cardGroup.DELETE("/:id", s.deleteCard)
	cardGroup.PUT("/:id/pin", s.updateCardPin)
	cardGroup.POST("/:id/freeze", s.freezeCard)
	cardGroup.POST("/:id/unfreeze", s.unfreezeCard)
	cardGroup.POST("/:id/reveal", s.revealCardDetails)
	cardGroup.PUT("/:id/3ds-challenge-method", s.updateCard3DSChallengeMethod)
	cardGroup.POST("/:id/activate", s.activatePhysicalCard)
	cardGroup.GET("/:id/activation-code", s.getCardActivationCode)
	cardGroup.POST("/:id/push-provisioning", s.pushProvisionCard)

	challengeGroup := s.echo.Group("/card-3ds-challenges", s.authService.RequireAuth)
	challengeGroup.GET("/:id", s.getCard3DSChallenge)
	challengeGroup.POST("/:id/respond", s.respondToCard3DSChallenge)

	shipmentGroup := s.echo.Group("/card-shipments", s.authService.RequireAuth)
	shipmentGroup.POST("", s.createCardShipment)
	shipmentGroup.GET("", s.listCardShipments)
	shipmentGroup.GET("/:id", s.getCardShipment)
	shipmentGroup.DELETE("/:id", s.deleteCardShipment)
	shipmentGroup.PATCH("/:id", s.updateCardShipment)
	shipmentGroup.POST("/:id/cards", s.addCardToShipment)
	shipmentGroup.DELETE("/:id/cards/:memberId", s.removeCardFromShipment)
	shipmentGroup.POST("/:id/submit", s.submitCardShipment)

	designGroup := s.echo.Group("/card-designs", s.authService.RequireAuth)
	designGroup.GET("", s.listCardDesigns)
	designGroup.GET("/:id", s.getCardDesign)

	transactionGroup := s.echo.Group("/card-transactions", s.authService.RequireAuth)
	transactionGroup.GET("/:id", s.getCardTransaction)

	s.echo.GET("/activities", s.listActivities, s.authService.RequireAuth)

	fraudAlertGroup := s.echo.Group("/fraud-alerts", s.authService.RequireAuth)
	fraudAlertGroup.POST("", s.reportFraud)
	fraudAlertGroup.GET("", s.listFraudAlerts)
	fraudAlertGroup.GET("/:id", s.getFraudAlert)
	fraudAlertGroup.POST("/:id/respond", s.respondToFraudAlert)

	// ponytail: registering the group only in sandbox is the local enforcement;
	// REAP itself also rejects these calls outside sandbox.
	if reapEnv == reap.EnvSandbox {
		simulationGroup := s.echo.Group("/simulation", s.authService.RequireAuth)
		simulationGroup.POST("/users/:id/application", s.simulateUserApplicationStatus)
		simulationGroup.POST("/companies/:id/status", s.simulateCompanyStatus)
		simulationGroup.POST("/accounts/:id/status", s.simulateAccountStatus)
		simulationGroup.POST("/cards/:id/status", s.simulateCardStatus)
		simulationGroup.POST("/card-transactions/authorization", s.simulateAuthorization)
		simulationGroup.POST("/card-transactions/authorization-with-3ds", s.simulateThreeDSAuthorization)
		simulationGroup.POST("/card-transactions/decline", s.simulateDecline)
		simulationGroup.POST("/card-transactions/clearing", s.simulateClearing)
		simulationGroup.POST("/card-transactions/reversal", s.simulateReversal)
		simulationGroup.POST("/card-transactions/refund", s.simulateRefund)
	}

	return s
}

// writeTimeout covers the handler, so it has to stay above the REAP client's
// own request timeout.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 120 * time.Second
)

func (s *Server) Start(addr string) error {
	// Configure Echo's own server, not one we pass to StartServer: Shutdown only
	// knows about e.Server.
	s.echo.Server.ReadHeaderTimeout = readHeaderTimeout
	s.echo.Server.ReadTimeout = readTimeout
	s.echo.Server.WriteTimeout = writeTimeout
	s.echo.Server.IdleTimeout = idleTimeout
	return s.echo.Start(addr)
}

// Shutdown stops accepting connections and waits for in-flight requests, until
// ctx is done.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

func (s *Server) health(c echo.Context) error {
	if err := s.pool.Ping(c.Request().Context()); err != nil {
		return c.String(http.StatusServiceUnavailable, "db unavailable")
	}
	return c.String(http.StatusOK, "ok")
}

func (s *Server) nonce(c echo.Context) error {
	publicKey := c.QueryParam("public_key")
	if publicKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	nonce, err := reapmapping.GetNonce(c.Request().Context(), s.pool, publicKey)
	if err != nil {
		log.Printf("nonce: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	}
	return c.JSON(http.StatusOK, echo.Map{"nonce": nonce})
}

func (s *Server) auth(c echo.Context) error {
	var req struct {
		PublicKey string `json:"public_key"`
		ChainCode string `json:"chain_code"`
		Nonce     int64  `json:"nonce"`
		Signature string `json:"signature"`
	}
	if err := c.Bind(&req); err != nil || req.PublicKey == "" || req.ChainCode == "" || req.Signature == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	token, err := s.authService.Authenticate(c.Request().Context(), req.PublicKey, req.ChainCode, req.Nonce, req.Signature)
	switch {
	case err == nil:
		return c.JSON(http.StatusOK, echo.Map{"access_token": token, "token_type": "Bearer"})
	case errors.Is(err, service.ErrInvalidSignature), errors.Is(err, service.ErrNonceUsed):
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid signature or nonce"})
	default:
		log.Printf("auth: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	}
}

// reapWebhook receives REAP's async webhook deliveries
// (https://docs.reap.global/webhooks/overview). It's unauthenticated by
// vault JWT — REAP proves the request is genuine via the HMAC-signed
// X-Reap-Webhook-Signature header instead, verified in
// WebhookService.HandleEvent.
func (s *Server) reapWebhook(c echo.Context) error {
	rawBody, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	err = s.webhookService.HandleEvent(c.Request().Context(), rawBody, c.Request().Header.Get("X-Reap-Webhook-Signature"))
	switch {
	case errors.Is(err, service.ErrInvalidWebhookSignature):
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid signature"})
	case errors.Is(err, service.ErrInvalidWebhookPayload):
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid payload"})
	case err != nil:
		log.Printf("reapWebhook: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.NoContent(http.StatusOK)
	}
}

func (s *Server) createUser(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)

	var req struct {
		Email                  string `json:"email"`
		PhoneNumber            string `json:"phoneNumber"`
		FirstName              string `json:"firstName"`
		LastName               string `json:"lastName"`
		TermsAcceptanceVersion string `json:"termsAcceptanceVersion"`
	}
	if err := c.Bind(&req); err != nil || req.Email == "" || req.PhoneNumber == "" || req.TermsAcceptanceVersion == "" {
		log.Printf("createUser: invalid request: bind_err=%v email_set=%t phone_set=%t terms_set=%t",
			err, req.Email != "", req.PhoneNumber != "", req.TermsAcceptanceVersion != "")
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	createReq := reap.CreateUserRequest{
		Email:       req.Email,
		PhoneNumber: req.PhoneNumber,
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		TermsAcceptance: reap.TermsAcceptance{
			Version:   req.TermsAcceptanceVersion,
			IPAddress: c.RealIP(),
		},
	}

	status, body, err := s.userService.CreateUser(c.Request().Context(), claims.PublicKey, createReq)
	switch {
	case errors.Is(err, service.ErrReapUserExists):
		return c.JSON(http.StatusConflict, echo.Map{"error": "reap user already exists"})
	case err != nil:
		log.Printf("createUser: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) getUser(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)

	status, body, err := s.userService.GetUser(c.Request().Context(), claims.PublicKey)
	switch {
	case errors.Is(err, service.ErrNoReapUser):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "no reap user for this vault"})
	case err != nil:
		log.Printf("getUser: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) updateUserEmail(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)

	var req struct {
		Email string `json:"email"`
	}
	if err := c.Bind(&req); err != nil || req.Email == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.userService.UpdateEmail(c.Request().Context(), claims.PublicKey, req.Email)
	switch {
	case errors.Is(err, service.ErrNoReapUser):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "no reap user for this vault"})
	case err != nil:
		log.Printf("updateUserEmail: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) updateUserPhone(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)

	var req struct {
		PhoneNumber string `json:"phoneNumber"`
	}
	if err := c.Bind(&req); err != nil || req.PhoneNumber == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.userService.UpdatePhoneNumber(c.Request().Context(), claims.PublicKey, req.PhoneNumber)
	switch {
	case errors.Is(err, service.ErrNoReapUser):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "no reap user for this vault"})
	case err != nil:
		log.Printf("updateUserPhone: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) advanceUserApplication(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	idempotencyKey := c.Request().Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Idempotency-Key header is required"})
	}

	status, respBody, err := s.userService.AdvanceUserApplication(c.Request().Context(), claims.PublicKey, body, idempotencyKey)
	switch {
	case errors.Is(err, service.ErrNoReapUser):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "no reap user for this vault"})
	case err != nil:
		log.Printf("advanceUserApplication: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, respBody)
	}
}

func (s *Server) createAccount(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)

	idempotencyKey := c.Request().Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Idempotency-Key header is required"})
	}

	var req struct {
		Signers *reap.Signers `json:"signers"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.accountService.CreateAccount(c.Request().Context(), claims.PublicKey, req.Signers, idempotencyKey)
	switch {
	case errors.Is(err, service.ErrNoReapUser):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "no reap user for this vault"})
	case err != nil:
		log.Printf("createAccount: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) listAccounts(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)

	limit, _ := strconv.Atoi(c.QueryParam("limit"))

	status, body, err := s.accountService.ListAccounts(c.Request().Context(), claims.PublicKey, limit, c.QueryParam("cursor"))
	switch {
	case errors.Is(err, service.ErrNoReapUser):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "no reap user for this vault"})
	case err != nil:
		log.Printf("listAccounts: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) generateSignerMessage(c echo.Context) error {
	status, body, err := s.accountService.GenerateSignerMessage(c.Request().Context())
	if err != nil {
		log.Printf("generateSignerMessage: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	}
	return c.Blob(status, echo.MIMEApplicationJSON, body)
}

func (s *Server) getAccount(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.accountService.GetAccount(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("getAccount: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) getAccountBalance(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.accountService.GetAccountBalance(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("getAccountBalance: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) getAccountAssets(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.accountService.GetAccountAssets(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("getAccountAssets: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) createCard(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)

	idempotencyKey := c.Request().Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Idempotency-Key header is required"})
	}

	var req struct {
		AccountID              string `json:"accountId"`
		Type                   string `json:"type"`
		ThreeDSChallengeMethod string `json:"3dsChallengeMethod"`
		CardDesignID           string `json:"cardDesignId"`
	}
	if err := c.Bind(&req); err != nil || req.AccountID == "" || req.Type == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	createReq := reap.CreateCardRequest{
		AccountID:              req.AccountID,
		Type:                   req.Type,
		ThreeDSChallengeMethod: req.ThreeDSChallengeMethod,
		CardDesignID:           req.CardDesignID,
	}

	status, body, err := s.cardService.CreateCard(c.Request().Context(), claims.PublicKey, createReq, idempotencyKey)
	switch {
	case errors.Is(err, service.ErrNoReapUser):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "no reap user for this vault"})
	case err != nil:
		log.Printf("createCard: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) listCards(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)

	status, body, err := s.cardService.ListCards(c.Request().Context(), claims.PublicKey, c.QueryParams())
	switch {
	case errors.Is(err, service.ErrNoReapUser):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "no reap user for this vault"})
	case err != nil:
		log.Printf("listCards: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) getCard(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardService.GetCard(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("getCard: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) deleteCard(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardService.DeleteCard(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("deleteCard: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) updateCardPin(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		Pin string `json:"pin"`
	}
	if err := c.Bind(&req); err != nil || req.Pin == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.cardService.UpdateCardPin(c.Request().Context(), claims.PublicKey, c.Param("id"), req.Pin)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("updateCardPin: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) freezeCard(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardService.FreezeCard(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("freezeCard: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) unfreezeCard(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardService.UnfreezeCard(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("unfreezeCard: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) revealCardDetails(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		StylesheetURL     string `json:"stylesheetUrl"`
		ShowCopyPanButton bool   `json:"showCopyPanButton"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.cardService.RevealCardDetails(c.Request().Context(), claims.PublicKey, c.Param("id"), req.StylesheetURL, req.ShowCopyPanButton)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("revealCardDetails: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) updateCard3DSChallengeMethod(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		ThreeDSChallengeMethod string `json:"3dsChallengeMethod"`
	}
	if err := c.Bind(&req); err != nil || req.ThreeDSChallengeMethod == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.cardService.UpdateCard3DSChallengeMethod(c.Request().Context(), claims.PublicKey, c.Param("id"), req.ThreeDSChallengeMethod)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("updateCard3DSChallengeMethod: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) activatePhysicalCard(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		ActivationCode string `json:"activationCode"`
	}
	if err := c.Bind(&req); err != nil || req.ActivationCode == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.cardService.ActivatePhysicalCard(c.Request().Context(), claims.PublicKey, c.Param("id"), req.ActivationCode)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("activatePhysicalCard: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) getCardActivationCode(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardService.GetCardActivationCode(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("getCardActivationCode: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) pushProvisionCard(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		Provider        string `json:"provider"`
		WalletAccountID string `json:"walletAccountId"`
		DeviceID        string `json:"deviceId"`
	}
	if err := c.Bind(&req); err != nil || req.Provider == "" || req.WalletAccountID == "" || req.DeviceID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	idempotencyKey := c.Request().Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Idempotency-Key header is required"})
	}

	status, body, err := s.cardService.PushProvisionCard(c.Request().Context(), claims.PublicKey, c.Param("id"), req.Provider, req.WalletAccountID, req.DeviceID, idempotencyKey)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("pushProvisionCard: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) respondToCard3DSChallenge(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		Approve *bool `json:"approve"`
	}
	if err := c.Bind(&req); err != nil || req.Approve == nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.cardService.RespondToCard3DSChallenge(c.Request().Context(), claims.PublicKey, c.Param("id"), *req.Approve)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("respondToCard3DSChallenge: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) getCard3DSChallenge(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardService.GetCard3DSChallenge(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("getCard3DSChallenge: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) createCardShipment(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req reap.CreateCardShipmentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.cardShipmentService.CreateCardShipment(c.Request().Context(), claims.PublicKey, req)
	switch {
	case errors.Is(err, service.ErrMissingScopeFilter):
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "at least one owned card is required"})
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("createCardShipment: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) listCardShipments(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardShipmentService.ListCardShipments(c.Request().Context(), claims.PublicKey, c.QueryParams())
	switch {
	case errors.Is(err, service.ErrMissingScopeFilter):
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "cardId filter is required"})
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("listCardShipments: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) getCardShipment(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardShipmentService.GetCardShipment(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("getCardShipment: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) deleteCardShipment(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardShipmentService.DeleteCardShipment(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("deleteCardShipment: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) updateCardShipment(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req reap.UpdateCardShipmentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.cardShipmentService.UpdateCardShipment(c.Request().Context(), claims.PublicKey, c.Param("id"), req)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("updateCardShipment: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) addCardToShipment(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req reap.CardShipmentMember
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.cardShipmentService.AddCardToShipment(c.Request().Context(), claims.PublicKey, c.Param("id"), req)
	switch {
	case errors.Is(err, service.ErrMissingScopeFilter):
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "cardId is required"})
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("addCardToShipment: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) removeCardFromShipment(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardShipmentService.RemoveCardFromShipment(c.Request().Context(), claims.PublicKey, c.Param("id"), c.Param("memberId"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("removeCardFromShipment: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) submitCardShipment(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	idempotencyKey := c.Request().Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Idempotency-Key header is required"})
	}

	status, body, err := s.cardShipmentService.SubmitCardShipment(c.Request().Context(), claims.PublicKey, c.Param("id"), idempotencyKey)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("submitCardShipment: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) listCardDesigns(c echo.Context) error {
	status, body, err := s.cardDesignService.ListCardDesigns(c.Request().Context(), c.QueryParams())
	if err != nil {
		log.Printf("listCardDesigns: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	}
	return c.Blob(status, echo.MIMEApplicationJSON, body)
}

func (s *Server) getCardDesign(c echo.Context) error {
	status, body, err := s.cardDesignService.GetCardDesign(c.Request().Context(), c.Param("id"))
	if err != nil {
		log.Printf("getCardDesign: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	}
	return c.Blob(status, echo.MIMEApplicationJSON, body)
}

func (s *Server) getCardTransaction(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.cardTransactionService.GetCardTransaction(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("getCardTransaction: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) listActivities(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.activityService.ListActivities(c.Request().Context(), claims.PublicKey, c.QueryParams())
	switch {
	case errors.Is(err, service.ErrMissingScopeFilter):
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "accountId or cardId filter is required"})
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("listActivities: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) reportFraud(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		TransactionID        string `json:"transactionId"`
		Type                 string `json:"type"`
		CardholderNotifiedAt string `json:"cardholderNotifiedAt"`
	}
	if err := c.Bind(&req); err != nil || req.TransactionID == "" || req.Type == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	idempotencyKey := c.Request().Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Idempotency-Key header is required"})
	}

	status, body, err := s.fraudAlertService.ReportFraud(c.Request().Context(), claims.PublicKey, reap.ReportFraudRequest{
		TransactionID:        req.TransactionID,
		Type:                 req.Type,
		CardholderNotifiedAt: req.CardholderNotifiedAt,
	}, idempotencyKey)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("reportFraud: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) listFraudAlerts(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.fraudAlertService.ListFraudAlerts(c.Request().Context(), claims.PublicKey, c.QueryParams())
	switch {
	case errors.Is(err, service.ErrMissingScopeFilter):
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "cardId or transactionId filter is required"})
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("listFraudAlerts: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) getFraudAlert(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	status, body, err := s.fraudAlertService.GetFraudAlert(c.Request().Context(), claims.PublicKey, c.Param("id"))
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("getFraudAlert: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) respondToFraudAlert(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		Response string `json:"response"`
		Type     string `json:"type"`
	}
	if err := c.Bind(&req); err != nil || req.Response == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	idempotencyKey := c.Request().Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Idempotency-Key header is required"})
	}

	status, body, err := s.fraudAlertService.RespondToFraudAlert(c.Request().Context(), claims.PublicKey, c.Param("id"), reap.RespondToFraudAlertRequest{
		Response: req.Response,
		Type:     req.Type,
	}, idempotencyKey)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("respondToFraudAlert: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateUserApplicationStatus(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		Status string `json:"status"`
	}
	if err := c.Bind(&req); err != nil || req.Status == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateUserApplicationStatus(c.Request().Context(), claims.PublicKey, c.Param("id"), req.Status)
	switch {
	case errors.Is(err, service.ErrNoReapUser):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "no reap user for this vault"})
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateUserApplicationStatus: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateCompanyStatus(c echo.Context) error {
	var req reap.SimulateCompanyStatusRequest
	if err := c.Bind(&req); err != nil || req.Status == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateCompanyStatus(c.Request().Context(), c.Param("id"), req)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateCompanyStatus: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateAccountStatus(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		Status string `json:"status"`
	}
	if err := c.Bind(&req); err != nil || req.Status == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateAccountStatus(c.Request().Context(), claims.PublicKey, c.Param("id"), req.Status)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateAccountStatus: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateCardStatus(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req struct {
		Status string `json:"status"`
	}
	if err := c.Bind(&req); err != nil || req.Status == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateCardStatus(c.Request().Context(), claims.PublicKey, c.Param("id"), req.Status)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateCardStatus: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateAuthorization(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req reap.SimulateCardTransactionRequest
	if err := c.Bind(&req); err != nil || req.CardID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateAuthorization(c.Request().Context(), claims.PublicKey, req)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateAuthorization: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateThreeDSAuthorization(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req reap.SimulateCardTransactionRequest
	if err := c.Bind(&req); err != nil || req.CardID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateThreeDSAuthorization(c.Request().Context(), claims.PublicKey, req)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateThreeDSAuthorization: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateDecline(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req reap.SimulateCardTransactionRequest
	if err := c.Bind(&req); err != nil || req.CardID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateDecline(c.Request().Context(), claims.PublicKey, req)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateDecline: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateClearing(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req reap.SimulateCardTransactionRequest
	if err := c.Bind(&req); err != nil || req.CardID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateClearing(c.Request().Context(), claims.PublicKey, req)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateClearing: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateReversal(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req reap.SimulateCardTransactionRequest
	if err := c.Bind(&req); err != nil || req.CardID == "" || req.TransactionID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateReversal(c.Request().Context(), claims.PublicKey, req)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateReversal: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}

func (s *Server) simulateRefund(c echo.Context) error {
	claims := c.Get("claims").(*service.Claims)
	var req reap.SimulateCardTransactionRequest
	if err := c.Bind(&req); err != nil || req.CardID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	status, body, err := s.simulationService.SimulateRefund(c.Request().Context(), claims.PublicKey, req)
	switch {
	case errors.Is(err, service.ErrResourceNotOwned):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not found"})
	case err != nil:
		log.Printf("simulateRefund: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	default:
		return c.Blob(status, echo.MIMEApplicationJSON, body)
	}
}
