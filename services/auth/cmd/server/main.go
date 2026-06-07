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

	sharedjwt "github.com/activialtd/gomarketi.com-backend/shared/pkg/jwt"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/email"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/handler"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/oauth"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/repository"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/service"
)

func main() {
	log := zerolog.New(os.Stdout).With().Timestamp().Logger()

	if err := run(log); err != nil {
		log.Fatal().Err(err).Msg("startup failed")
	}
}

func run(log zerolog.Logger) error {
	// ── Config ────────────────────────────────────────────────────────────────
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()
	_ = viper.ReadInConfig() // .env is optional in prod (env vars take precedence)

	dbURL := viper.GetString("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	jwtPrivKeyPath := viper.GetString("JWT_PRIVATE_KEY_PATH")
	jwtPubKeyPath := viper.GetString("JWT_PUBLIC_KEY_PATH")
	accessTTL := time.Duration(viper.GetInt("JWT_ACCESS_TTL_SECONDS")) * time.Second
	if accessTTL == 0 {
		accessTTL = 15 * time.Minute
	}
	refreshTTL := time.Duration(viper.GetInt("JWT_REFRESH_TTL_SECONDS")) * time.Second
	if refreshTTL == 0 {
		refreshTTL = 30 * 24 * time.Hour
	}

	// ── Database ──────────────────────────────────────────────────────────────
	db, err := connectDB(dbURL, log)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	// ── JWT ───────────────────────────────────────────────────────────────────
	privKey, err := sharedjwt.LoadPrivateKey(jwtPrivKeyPath)
	if err != nil {
		return fmt.Errorf("load jwt private key: %w", err)
	}
	pubKey, err := sharedjwt.LoadPublicKey(jwtPubKeyPath)
	if err != nil {
		return fmt.Errorf("load jwt public key: %w", err)
	}
	jwtManager, err := sharedjwt.NewManager(sharedjwt.Config{
		PrivateKey:     privKey,
		PublicKey:      pubKey,
		AccessTokenTTL: accessTTL,
	})
	if err != nil {
		return fmt.Errorf("jwt manager: %w", err)
	}

	// ── Email ─────────────────────────────────────────────────────────────────
	// Use SMTP (Gmail) when SMTP_HOST is set, otherwise fall back to Mailgun.
	var emailer email.Emailer
	if viper.GetString("SMTP_HOST") != "" {
		emailer, err = email.NewSMTPClient(email.SMTPConfig{
			Host:     viper.GetString("SMTP_HOST"),
			Port:     viper.GetString("SMTP_PORT"),
			Username: viper.GetString("SMTP_USERNAME"),
			Password: viper.GetString("SMTP_PASSWORD"),
			From:     viper.GetString("SMTP_FROM"),
		})
		if err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
		log.Info().Str("host", viper.GetString("SMTP_HOST")).Msg("using SMTP emailer")
	} else {
		emailer, err = email.NewMailgunClient(email.MailgunConfig{
			APIKey: viper.GetString("MAILGUN_API_KEY"),
			Domain: viper.GetString("MAILGUN_DOMAIN"),
			From:   viper.GetString("MAILGUN_FROM"),
		})
		if err != nil {
			return fmt.Errorf("mailgun client: %w", err)
		}
		log.Info().Msg("using Mailgun emailer")
	}

	// ── OAuth verifiers ───────────────────────────────────────────────────────
	googleVerifier := oauth.NewGoogleVerifier(viper.GetString("GOOGLE_CLIENT_ID"))
	appleVerifier := oauth.NewAppleVerifier(viper.GetString("APPLE_BUNDLE_ID"))

	// ── Wire up layers ────────────────────────────────────────────────────────
	store := repository.NewStore(db)
	svc := service.New(store, jwtManager, emailer, googleVerifier, appleVerifier, log)

	isProduction := viper.GetString("ENV") == "production"
	if isProduction {
		gin.SetMode(gin.ReleaseMode)
	}

	h := handler.New(svc, refreshTTL, isProduction)

	r := gin.New()
	allowedOrigins := viper.GetStringSlice("ALLOWED_ORIGINS")
	handler.Register(r, h, log, allowedOrigins)

	// ── HTTP server ───────────────────────────────────────────────────────────
	port := viper.GetString("PORT")
	if port == "" {
		port = "8080"
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
		log.Info().Str("addr", srv.Addr).Msg("auth service listening")
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
