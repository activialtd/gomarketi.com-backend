// Package service contains all auth business logic.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	sharedjwt "github.com/activialtd/gomarketi.com-backend/shared/pkg/jwt"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/domain"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/dto"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/email"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/oauth"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/repository"
	"github.com/activialtd/gomarketi.com-backend/services/auth/internal/repository/db"
)

const refreshTTL = 30 * 24 * time.Hour

// AuthService implements all authentication use-cases.
type AuthService struct {
	store   *repository.Store
	jwt     *sharedjwt.Manager
	emailer email.Emailer
	google  *oauth.GoogleVerifier
	apple   *oauth.AppleVerifier
	log     zerolog.Logger
}

// New creates an AuthService.
func New(
	store *repository.Store,
	jwtManager *sharedjwt.Manager,
	emailer email.Emailer,
	google *oauth.GoogleVerifier,
	apple *oauth.AppleVerifier,
	log zerolog.Logger,
) *AuthService {
	return &AuthService{
		store:   store,
		jwt:     jwtManager,
		emailer: emailer,
		google:  google,
		apple:   apple,
		log:     log,
	}
}

// ── Password-based auth ───────────────────────────────────────────────────────

// Register creates a new account with email + password.
// Returns an error if the email is already taken (by any auth method).
func (s *AuthService) Register(ctx context.Context, req dto.RegisterReq) (dto.AuthResp, string, error) {
	if req.Password != req.ConfirmPassword {
		return dto.AuthResp{}, "", apperrors.BadRequest("passwords do not match")
	}
	if !req.TermsAccepted {
		return dto.AuthResp{}, "", apperrors.BadRequest("you must accept the terms of service")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("hash password: %w", err))
	}

	fullName := req.FirstName + " " + req.LastName

	var (
		user         db.User
		refreshToken string
	)

	err = s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		var txErr error
		user, txErr = qtx.CreateUserWithPassword(ctx, db.CreateUserWithPasswordParams{
			Email:            req.Email,
			FullName:         sql.NullString{String: fullName, Valid: true},
			PasswordHash:     string(hash),
			TermsAccepted:    req.TermsAccepted,
			MarketingConsent: req.MarketingConsent,
		})
		if txErr != nil {
			if isUniqueViolation(txErr) {
				return apperrors.Conflict("an account with this email already exists")
			}
			return fmt.Errorf("create user: %w", txErr)
		}

		_, txErr = qtx.UpsertEmailIdentity(ctx, user.ID, user.ID.String())
		if txErr != nil {
			return fmt.Errorf("upsert identity: %w", txErr)
		}

		if txErr = qtx.EnsureBuyerProfile(ctx, user.ID); txErr != nil {
			return fmt.Errorf("ensure buyer profile: %w", txErr)
		}

		refreshToken, txErr = s.issueRefreshToken(ctx, qtx, user.ID, uuid.New(), "", "", "")
		return txErr
	})
	if err != nil {
		// Propagate AppError (e.g. Conflict) directly.
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return dto.AuthResp{}, "", err
		}
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("register: %w", err))
	}

	accessToken, err := s.issueAccessToken(ctx, user, true, false)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("issue token: %w", err))
	}

	return dto.AuthResp{
		AccessToken: accessToken,
		User:        userToDTO(user, true, false),
	}, refreshToken, nil
}

// LoginWithPassword authenticates with email + password.
// Returns a distinct error when the account was created via Google/Apple
// so the frontend can show the right prompt.
func (s *AuthService) LoginWithPassword(ctx context.Context, req dto.LoginReq) (dto.AuthResp, string, error) {
	loginUser, err := s.store.Queries().GetUserForLogin(ctx, req.Email)
	if err != nil {
		// Use a generic message to avoid revealing whether the email is registered.
		return dto.AuthResp{}, "", apperrors.Unauthorized("invalid email or password")
	}

	if !loginUser.IsActive {
		return dto.AuthResp{}, "", apperrors.Unauthorized("account is disabled")
	}

	// Account exists but was created via Google/Apple — no password was ever set.
	if !loginUser.PasswordHash.Valid {
		return dto.AuthResp{}, "", apperrors.BadRequest("this account was created with Google or Apple sign-in — please use those options to log in")
	}

	if err = bcrypt.CompareHashAndPassword([]byte(loginUser.PasswordHash.String), []byte(req.Password)); err != nil {
		return dto.AuthResp{}, "", apperrors.Unauthorized("invalid email or password")
	}

	user := loginUser.ToUser()

	var refreshToken string
	err = s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		var txErr error
		if txErr = qtx.UpdateLastLogin(ctx, user.ID); txErr != nil {
			return txErr
		}
		refreshToken, txErr = s.issueRefreshToken(ctx, qtx, user.ID, uuid.New(), "", "", "")
		return txErr
	})
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("login: %w", err))
	}

	isBuyer := false
	_, bErr := s.store.Queries().GetBuyerProfileByUserID(ctx, user.ID)
	if bErr == nil {
		isBuyer = true
	}

	accessToken, err := s.issueAccessToken(ctx, user, isBuyer, false)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("issue token: %w", err))
	}

	return dto.AuthResp{
		AccessToken: accessToken,
		User:        userToDTO(user, isBuyer, false),
	}, refreshToken, nil
}

// ── OTP ───────────────────────────────────────────────────────────────────────

// RequestOTP generates a 6-digit OTP, stores it hashed, and emails it.
func (s *AuthService) RequestOTP(ctx context.Context, req dto.OTPRequestReq) (dto.OTPRequestResp, error) {
	otp, err := domain.GenerateOTP()
	if err != nil {
		return dto.OTPRequestResp{}, apperrors.Internal(fmt.Errorf("generate otp: %w", err))
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(otp), bcrypt.DefaultCost)
	if err != nil {
		return dto.OTPRequestResp{}, apperrors.Internal(fmt.Errorf("hash otp: %w", err))
	}

	sessionToken := uuid.New().String()
	expiresAt := time.Now().UTC().Add(domain.OTPExpiry)

	_, err = s.store.Queries().CreateOTPSession(ctx, db.CreateOTPSessionParams{
		Email:        req.Email,
		SessionToken: sessionToken,
		OtpHash:      string(hash),
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		return dto.OTPRequestResp{}, apperrors.Internal(fmt.Errorf("create otp session: %w", err))
	}

	if err = s.emailer.SendOTP(ctx, req.Email, otp); err != nil {
		s.log.Error().Err(err).Str("email", req.Email).Msg("failed to send otp email")
		return dto.OTPRequestResp{}, apperrors.Internal(fmt.Errorf("send otp: %w", err))
	}

	return dto.OTPRequestResp{
		SessionToken: sessionToken,
		ExpiresIn:    int(domain.OTPExpiry.Seconds()),
	}, nil
}

// VerifyOTP checks the OTP, creates or updates the user, and issues tokens.
func (s *AuthService) VerifyOTP(ctx context.Context, req dto.OTPVerifyReq) (dto.AuthResp, string, error) {
	q := s.store.Queries()

	session, err := q.GetOTPSessionByToken(ctx, req.SessionToken)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Unauthorized("invalid session token")
	}

	// Map db row → domain entity.
	otpSession := domain.OTPSession{
		ID:        session.ID.String(),
		Email:     session.Email,
		OTPHash:   session.OtpHash,
		Attempts:  int(session.Attempts),
		ExpiresAt: session.ExpiresAt,
	}
	if session.UsedAt.Valid {
		t := session.UsedAt.Time
		otpSession.UsedAt = &t
	}

	if err = otpSession.ValidateForVerification(); err != nil {
		switch {
		case errors.Is(err, domain.ErrOTPAlreadyUsed):
			return dto.AuthResp{}, "", apperrors.Unauthorized("otp already used")
		case errors.Is(err, domain.ErrOTPExpired):
			return dto.AuthResp{}, "", apperrors.Unauthorized("otp expired")
		case errors.Is(err, domain.ErrOTPExhausted):
			return dto.AuthResp{}, "", apperrors.TooManyRequests("too many attempts")
		}
	}

	// Increment attempts BEFORE bcrypt.Compare (timing attack prevention).
	updatedSession, err := q.IncrementOTPAttempts(ctx, session.ID)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("increment attempts: %w", err))
	}

	if err = bcrypt.CompareHashAndPassword([]byte(updatedSession.OtpHash), []byte(req.OTP)); err != nil {
		if int(updatedSession.Attempts) >= domain.MaxOTPAttempts {
			return dto.AuthResp{}, "", apperrors.TooManyRequests("too many attempts")
		}
		return dto.AuthResp{}, "", apperrors.Unauthorized("invalid otp")
	}

	var (
		user         db.User
		refreshToken string
	)

	err = s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		var txErr error
		user, txErr = qtx.UpsertUserByEmail(ctx, db.UpsertUserByEmailParams{
			Email:           session.Email,
			IsEmailVerified: true,
		})
		if txErr != nil {
			return fmt.Errorf("upsert user: %w", txErr)
		}

		_, txErr = qtx.UpsertEmailIdentity(ctx, user.ID, user.ID.String())
		if txErr != nil {
			return fmt.Errorf("upsert identity: %w", txErr)
		}

		if txErr = qtx.MarkOTPSessionUsed(ctx, session.ID); txErr != nil {
			return fmt.Errorf("mark session used: %w", txErr)
		}

		if txErr = qtx.EnsureBuyerProfile(ctx, user.ID); txErr != nil {
			return fmt.Errorf("ensure buyer profile: %w", txErr)
		}

		refreshToken, txErr = s.issueRefreshToken(ctx, qtx, user.ID, uuid.New(), req.DeviceID, req.UserAgent, req.IPAddress)
		return txErr
	})
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("complete login: %w", err))
	}

	accessToken, err := s.issueAccessToken(ctx, user, false, false)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("issue access token: %w", err))
	}

	return dto.AuthResp{
		AccessToken: accessToken,
		User:        userToDTO(user, false, false),
	}, refreshToken, nil
}

// ── OAuth ─────────────────────────────────────────────────────────────────────

// AuthGoogle verifies a Google id_token and logs in or registers the user.
func (s *AuthService) AuthGoogle(ctx context.Context, req dto.GoogleAuthReq) (dto.AuthResp, string, error) {
	claims, err := s.google.Verify(req.IDToken)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Unauthorized("invalid google token")
	}

	var (
		user         db.User
		refreshToken string
	)

	err = s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		var txErr error
		user, txErr = qtx.UpsertUserByEmail(ctx, db.UpsertUserByEmailParams{
			Email:           claims.Email,
			FullName:        toNullString(claims.Name),
			AvatarUrl:       toNullString(claims.Picture),
			IsEmailVerified: claims.EmailVerified,
		})
		if txErr != nil {
			return fmt.Errorf("upsert user: %w", txErr)
		}

		existing, identErr := qtx.GetIdentityByProviderUID(ctx, "google", claims.Sub)
		if identErr != nil && !errors.Is(identErr, sql.ErrNoRows) {
			return fmt.Errorf("get identity: %w", identErr)
		}

		if errors.Is(identErr, sql.ErrNoRows) {
			_, txErr = qtx.CreateOAuthIdentity(ctx, db.CreateOAuthIdentityParams{
				UserID:         user.ID,
				Provider:       "google",
				ProviderUID:    claims.Sub,
				ProviderEmail:  toNullString(claims.Email),
				ProviderName:   toNullString(claims.Name),
				ProviderAvatar: toNullString(claims.Picture),
				IsPrimary:      true,
			})
		} else {
			txErr = qtx.UpdateIdentityLastUsed(ctx, existing.ID)
		}
		if txErr != nil {
			return txErr
		}

		if txErr = qtx.EnsureBuyerProfile(ctx, user.ID); txErr != nil {
			return fmt.Errorf("ensure buyer profile: %w", txErr)
		}

		refreshToken, txErr = s.issueRefreshToken(ctx, qtx, user.ID, uuid.New(), req.DeviceID, "", "")
		return txErr
	})
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("google login: %w", err))
	}

	accessToken, err := s.issueAccessToken(ctx, user, false, false)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("issue access token: %w", err))
	}

	return dto.AuthResp{
		AccessToken: accessToken,
		User:        userToDTO(user, false, false),
	}, refreshToken, nil
}

// AuthApple verifies an Apple identity_token and logs in or registers the user.
func (s *AuthService) AuthApple(ctx context.Context, req dto.AppleAuthReq) (dto.AuthResp, string, error) {
	claims, err := s.apple.Verify(req.IdentityToken)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Unauthorized("invalid apple token")
	}

	var (
		user         db.User
		refreshToken string
	)

	err = s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		existing, identErr := qtx.GetIdentityByProviderUID(ctx, "apple", claims.Sub)
		isNew := errors.Is(identErr, sql.ErrNoRows)
		if identErr != nil && !isNew {
			return fmt.Errorf("get identity: %w", identErr)
		}

		emailToUse := claims.Email
		if req.Email != nil && *req.Email != "" {
			emailToUse = *req.Email
		}

		var txErr error
		upsertParams := db.UpsertUserByEmailParams{
			Email:           emailToUse,
			IsEmailVerified: true,
		}
		if isNew && req.FullName != nil {
			upsertParams.FullName = toNullString(*req.FullName)
		}

		user, txErr = qtx.UpsertUserByEmail(ctx, upsertParams)
		if txErr != nil {
			return fmt.Errorf("upsert user: %w", txErr)
		}

		if isNew {
			nameCapt := req.FullName != nil
			_, txErr = qtx.CreateOAuthIdentity(ctx, db.CreateOAuthIdentityParams{
				UserID:            user.ID,
				Provider:          "apple",
				ProviderUID:       claims.Sub,
				ProviderEmail:     toNullString(emailToUse),
				AppleNameCaptured: nameCapt,
				IsPrimary:         true,
			})
		} else {
			txErr = qtx.UpdateIdentityLastUsed(ctx, existing.ID)
		}
		if txErr != nil {
			return txErr
		}

		if txErr = qtx.EnsureBuyerProfile(ctx, user.ID); txErr != nil {
			return fmt.Errorf("ensure buyer profile: %w", txErr)
		}

		refreshToken, txErr = s.issueRefreshToken(ctx, qtx, user.ID, uuid.New(), req.DeviceID, "", "")
		return txErr
	})
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("apple login: %w", err))
	}

	accessToken, err := s.issueAccessToken(ctx, user, false, false)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("issue access token: %w", err))
	}

	return dto.AuthResp{
		AccessToken: accessToken,
		User:        userToDTO(user, false, false),
	}, refreshToken, nil
}

// ── Token lifecycle ───────────────────────────────────────────────────────────

// RefreshTokens rotates a refresh token. Reuse of a revoked token revokes the whole family.
func (s *AuthService) RefreshTokens(ctx context.Context, rawToken string) (dto.AuthResp, string, error) {
	hash := domain.HashToken(rawToken)

	existing, err := s.store.Queries().GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Unauthorized("invalid refresh token")
	}

	if existing.RevokedAt.Valid {
		_ = s.store.Queries().RevokeTokenFamily(ctx, existing.FamilyID)
		return dto.AuthResp{}, "", apperrors.Unauthorized("refresh token reuse detected")
	}

	if time.Now().UTC().After(existing.ExpiresAt) {
		return dto.AuthResp{}, "", apperrors.Unauthorized("refresh token expired")
	}

	var (
		user         db.User
		refreshToken string
	)

	err = s.store.ExecTx(ctx, func(qtx *db.Queries) error {
		var txErr error
		if txErr = qtx.RevokeRefreshToken(ctx, existing.ID); txErr != nil {
			return txErr
		}

		user, txErr = qtx.GetUserByID(ctx, existing.UserID)
		if txErr != nil {
			return txErr
		}

		refreshToken, txErr = s.issueRefreshToken(ctx, qtx, existing.UserID, existing.FamilyID,
			existing.DeviceID.String, existing.UserAgent.String, existing.IpAddress.String)
		return txErr
	})
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("rotate refresh token: %w", err))
	}

	accessToken, err := s.issueAccessToken(ctx, user, false, false)
	if err != nil {
		return dto.AuthResp{}, "", apperrors.Internal(fmt.Errorf("issue access token: %w", err))
	}

	return dto.AuthResp{
		AccessToken: accessToken,
		User:        userToDTO(user, false, false),
	}, refreshToken, nil
}

// Logout revokes the presented refresh token (idempotent).
func (s *AuthService) Logout(ctx context.Context, rawToken string) error {
	hash := domain.HashToken(rawToken)
	existing, err := s.store.Queries().GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		return nil
	}
	return s.store.Queries().RevokeRefreshToken(ctx, existing.ID)
}

// ValidateToken parses a JWT access token — called by Envoy ext_authz.
func (s *AuthService) ValidateToken(_ context.Context, accessToken string) (dto.ValidateTokenResp, error) {
	claims, err := s.jwt.ValidateClaims(accessToken)
	if err != nil {
		return dto.ValidateTokenResp{}, apperrors.Unauthorized("invalid access token")
	}

	return dto.ValidateTokenResp{
		UserID:   claims.Subject,
		IsBuyer:  claims.IsBuyer,
		IsVendor: claims.IsVendor,
		StoreIDs: claims.StoreIDs,
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (s *AuthService) issueRefreshToken(
	ctx context.Context,
	qtx *db.Queries,
	userID, familyID uuid.UUID,
	deviceID, userAgent, ipAddress string,
) (string, error) {
	raw, hash, err := domain.GenerateRefreshToken()
	if err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}

	_, err = qtx.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: hash,
		FamilyID:  familyID,
		DeviceID:  toNullString(deviceID),
		UserAgent: toNullString(userAgent),
		IpAddress: toNullString(ipAddress),
		ExpiresAt: time.Now().UTC().Add(refreshTTL),
	})
	if err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}

	return raw, nil
}

// StaffLogin authenticates a store staff member with email + password.
// Issues a JWT with StoreIDs set to the staff's store and StaffRole populated.
func (s *AuthService) StaffLogin(ctx context.Context, req dto.LoginReq) (dto.AuthResp, error) {
	staff, err := s.store.QueryStaffByEmail(ctx, req.Email)
	if err != nil {
		return dto.AuthResp{}, apperrors.Unauthorized("invalid email or password")
	}
	if !staff.IsActive {
		return dto.AuthResp{}, apperrors.Unauthorized("this staff account has been disabled")
	}
	if staff.PasswordHash == "" {
		return dto.AuthResp{}, apperrors.BadRequest("password not set for this staff account — contact your store owner")
	}
	if err = bcrypt.CompareHashAndPassword([]byte(staff.PasswordHash), []byte(req.Password)); err != nil {
		return dto.AuthResp{}, apperrors.Unauthorized("invalid email or password")
	}

	claims := sharedjwt.Claims{
		IsBuyer:   false,
		IsVendor:  false,
		StoreIDs:  []string{staff.StoreID},
		StaffRole: staff.Role,
	}
	accessToken, err := s.jwt.IssueAccessToken(staff.ID, claims)
	if err != nil {
		return dto.AuthResp{}, apperrors.Internal(fmt.Errorf("issue staff token: %w", err))
	}

	email := staff.Email
	return dto.AuthResp{
		AccessToken: accessToken,
		User: dto.UserDTO{
			ID:      staff.ID,
			Email:   &email,
			IsBuyer: false,
			IsVendor: false,
		},
	}, nil
}

func (s *AuthService) issueAccessToken(ctx context.Context, user db.User, isBuyer, isVendor bool) (string, error) {
	// Embed store IDs so the gateway reads them directly from the JWT claim —
	// no storefront HTTP lookup needed for users who already have a store.
	storeIDs := s.store.QueryStoreIDs(ctx, user.ID.String())
	claims := sharedjwt.Claims{
		IsBuyer:  isBuyer,
		IsVendor: isVendor,
		StoreIDs: storeIDs,
	}
	return s.jwt.IssueAccessToken(user.ID.String(), claims)
}

func userToDTO(user db.User, isBuyer, isVendor bool) dto.UserDTO {
	u := dto.UserDTO{
		ID:               user.ID.String(),
		IsEmailVerified:  user.IsEmailVerified,
		ProfileCompleted: user.ProfileCompleted,
		IsBuyer:          isBuyer,
		IsVendor:         isVendor,
	}
	if user.Email.Valid {
		u.Email = &user.Email.String
	}
	if user.FullName.Valid {
		u.FullName = &user.FullName.String
	}
	if user.AvatarUrl.Valid {
		u.AvatarURL = &user.AvatarUrl.String
	}
	return u
}

func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505), which happens when a duplicate email is inserted.
func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}
