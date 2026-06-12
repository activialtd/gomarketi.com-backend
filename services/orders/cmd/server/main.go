package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"

	orddb "github.com/activialtd/gomarketi.com-backend/services/orders/db"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/handler"
	"github.com/activialtd/gomarketi.com-backend/services/orders/internal/service"
)

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Logger()
	if err := run(log); err != nil {
		log.Fatal().Err(err).Msg("startup failed")
	}
}

func run(log zerolog.Logger) error {
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()
	_ = viper.ReadInConfig()

	if viper.GetString("ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	dbURL := viper.GetString("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	db, err := connectDB(dbURL, log)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := orddb.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	log.Info().Msg("migrations applied")

	svc := service.New(db, log)
	h := handler.New(svc)
	r := gin.New()

	allowedOrigins := viper.GetStringSlice("ALLOWED_ORIGINS")
	handler.Register(r, h, log, allowedOrigins)

	port := viper.GetString("PORT")
	if port == "" {
		port = "8084"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", srv.Addr).Msg("orders service listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case sig := <-quit:
		log.Info().Str("signal", sig.String()).Msg("shutting down")
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}

func connectDB(dsn string, log zerolog.Logger) (*sqlx.DB, error) {
	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	for i := 1; i <= 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			log.Info().Msg("orders db connected")
			return db, nil
		}
		log.Warn().Err(err).Int("attempt", i).Msg("db ping failed, retrying")
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("database unreachable: %w", err)
}
