package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/activialtd/gomarketi.com-backend/shared/pkg/middleware"
)

// Register mounts all auth routes onto r.
func Register(r *gin.Engine, h *Handler, log zerolog.Logger, allowedOrigins []string) {
	r.Use(
		middleware.Recovery(log),
		middleware.RequestID(),
		middleware.RequestLogger(log),
		middleware.CORS(allowedOrigins),
	)

	v1 := r.Group("/v1")
	{
		auth := v1.Group("/auth")
		{
			// Password-based registration and login
			auth.POST("/register", h.Register)
			auth.POST("/login", h.Login)

			// Staff login — separate from vendor/buyer login
			staff := auth.Group("/staff")
			staff.POST("/login", h.StaffLogin)

			otp := auth.Group("/otp")
			otp.POST("/request", h.RequestOTP)
			otp.POST("/verify", h.VerifyOTP)

			oauthGroup := auth.Group("/oauth")
			oauthGroup.POST("/google", h.AuthGoogle)
			oauthGroup.POST("/apple", h.AuthApple)

			token := auth.Group("/token")
			token.POST("/refresh", h.RefreshTokens)

			auth.POST("/logout", h.Logout)
		}

		internal := v1.Group("/internal")
		internal.POST("/validate-token", h.ValidateToken)
	}
}
