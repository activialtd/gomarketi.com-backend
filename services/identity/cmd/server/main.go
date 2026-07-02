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

	pkgcrypto "github.com/activialtd/gomarketi.com-backend/shared/pkg/crypto"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/handler"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/repository"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/service"
	"github.com/activialtd/gomarketi.com-backend/services/identity/internal/smileid"
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

	dbURL := viper.GetString("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	encKeyHex := viper.GetString("ENCRYPTION_KEY")
	if encKeyHex == "" {
		return fmt.Errorf("ENCRYPTION_KEY is required")
	}
	encKey, err := pkgcrypto.ParseKey(encKeyHex)
	if err != nil {
		return fmt.Errorf("parse encryption key: %w", err)
	}

	db, err := connectDB(dbURL, log)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	store := repository.NewStore(db)

	// Smile ID KYC client — runs in simulation mode when credentials are not set.
	// Set SMILE_ID_PARTNER_ID + SMILE_ID_API_KEY env vars for live verification.
	// Set SMILE_ID_SANDBOX=true to use the Smile ID sandbox environment.
	kycClient := smileid.New(
		viper.GetString("SMILE_ID_PARTNER_ID"),
		viper.GetString("SMILE_ID_API_KEY"),
		viper.GetBool("SMILE_ID_SANDBOX"),
		log,
	)

	svc := service.New(store, encKey, kycClient, log)

	isProduction := viper.GetString("ENV") == "production"
	if isProduction {
		gin.SetMode(gin.ReleaseMode)
	}

	h := handler.New(svc)
	r := gin.New()
	allowedOrigins := viper.GetStringSlice("ALLOWED_ORIGINS")
	handler.Register(r, h, log, allowedOrigins)

	port := viper.GetString("PORT")
	if port == "" {
		port = "8081"
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
		log.Info().Str("addr", srv.Addr).Msg("identity service listening")
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
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
			log.Info().Msg("database connected")
			return db, nil
		}
		log.Warn().Err(err).Int("attempt", i).Msg("database ping failed, retrying")
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("database unreachable after 5 attempts: %w", err)
}
