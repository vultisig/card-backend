package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/reapmapping"
	"github.com/vultisig/card-backend/internal/service"
)

type Server struct {
	echo           *echo.Echo
	pool           *pgxpool.Pool
	authService    *service.AuthService
	userService    *service.UserService
	accountService *service.AccountService
}

func NewServer(pool *pgxpool.Pool, authService *service.AuthService, userService *service.UserService, accountService *service.AccountService) *Server {
	s := &Server{
		echo:           echo.New(),
		pool:           pool,
		authService:    authService,
		userService:    userService,
		accountService: accountService,
	}

	s.echo.Use(middleware.Recover())
	s.echo.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true,
		LogURI:    true,
		LogMethod: true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			log.Printf("%s %s %d", v.Method, v.URI, v.Status)
			return nil
		},
	}))
	s.echo.Use(middleware.RequestID())
	s.echo.Use(middleware.CORS())
	s.echo.Use(middleware.Secure())
	s.echo.Use(middleware.Gzip())

	s.echo.GET("/health", s.health)
	s.echo.GET("/nonce", s.nonce)
	s.echo.POST("/auth", s.auth)

	userGroup := s.echo.Group("/user", s.authService.RequireAuth)
	userGroup.POST("", s.createUser)
	userGroup.GET("", s.getUser)
	userGroup.PUT("/email", s.updateUserEmail)
	userGroup.PUT("/phone", s.updateUserPhone)

	accountGroup := s.echo.Group("/account", s.authService.RequireAuth)
	accountGroup.POST("", s.createAccount)
	accountGroup.GET("", s.listAccounts)
	accountGroup.GET("/signer-message", s.generateSignerMessage)
	accountGroup.GET("/:id", s.getAccount)
	accountGroup.GET("/:id/balance", s.getAccountBalance)
	accountGroup.GET("/:id/assets", s.getAccountAssets)

	return s
}

func (s *Server) Start(addr string) error {
	return s.echo.Start(addr)
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
		Nonce     int64  `json:"nonce"`
		Signature string `json:"signature"`
	}
	if err := c.Bind(&req); err != nil || req.PublicKey == "" || req.Signature == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	token, err := s.authService.Authenticate(c.Request().Context(), req.PublicKey, req.Nonce, req.Signature)
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
	status, body, err := s.accountService.GetAccount(c.Request().Context(), c.Param("id"))
	if err != nil {
		log.Printf("getAccount: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	}
	return c.Blob(status, echo.MIMEApplicationJSON, body)
}

func (s *Server) getAccountBalance(c echo.Context) error {
	status, body, err := s.accountService.GetAccountBalance(c.Request().Context(), c.Param("id"))
	if err != nil {
		log.Printf("getAccountBalance: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	}
	return c.Blob(status, echo.MIMEApplicationJSON, body)
}

func (s *Server) getAccountAssets(c echo.Context) error {
	status, body, err := s.accountService.GetAccountAssets(c.Request().Context(), c.Param("id"))
	if err != nil {
		log.Printf("getAccountAssets: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	}
	return c.Blob(status, echo.MIMEApplicationJSON, body)
}
