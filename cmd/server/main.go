package main

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/vultisig/card-backend/internal/config"
)

func main() {
	cfg := config.Load()

	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true,
		LogURI:    true,
		LogMethod: true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			log.Printf("%s %s %d", v.Method, v.URI, v.Status)
			return nil
		},
	}))
	e.Use(middleware.RequestID())
	e.Use(middleware.CORS())
	e.Use(middleware.Secure())
	e.Use(middleware.Gzip())

	e.GET("/health", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	log.Printf("card-backend starting on port %s", cfg.Port)
	log.Fatal(e.Start(":" + cfg.Port))
}
