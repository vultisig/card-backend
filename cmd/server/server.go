package main

import (
	"errors"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/vultisig/card-backend/internal/card"
	"github.com/vultisig/card-backend/internal/service"
)

type Server struct {
	echo        *echo.Echo
	pool        *pgxpool.Pool
	authService *service.AuthService
}

func NewServer(pool *pgxpool.Pool, authService *service.AuthService) *Server {
	s := &Server{
		echo:        echo.New(),
		pool:        pool,
		authService: authService,
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
	s.echo.POST("/auth", s.auth)

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
	case errors.Is(err, card.ErrNotFound):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "card not found"})
	case errors.Is(err, service.ErrCardNotActive):
		return c.JSON(http.StatusForbidden, echo.Map{"error": "card not active"})
	case errors.Is(err, service.ErrInvalidSignature), errors.Is(err, service.ErrNonceUsed):
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid signature or nonce"})
	default:
		log.Printf("auth: %v", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal error"})
	}
}
