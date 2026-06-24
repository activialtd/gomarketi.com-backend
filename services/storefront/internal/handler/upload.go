package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
)

// r2Client returns an S3-compatible client pointed at Cloudflare R2.
// Returns nil if R2 is not configured (optional in dev).
func r2Client() *s3.Client {
	accountID := os.Getenv("R2_ACCOUNT_ID")
	accessKey := os.Getenv("R2_ACCESS_KEY_ID")
	secretKey := os.Getenv("R2_SECRET_ACCESS_KEY")
	if accountID == "" || accessKey == "" || secretKey == "" {
		return nil
	}
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
}

// PresignUpload godoc
// POST /v1/storefront/uploads/presign
// Returns a presigned PUT URL so the client can upload directly to R2.
func (h *Handler) PresignUpload(c *gin.Context) {
	_, ok := h.callerID(c) // requires authenticated user
	if !ok {
		return
	}

	var req dto.PresignUploadReq
	if !h.bind(c, &req) {
		return
	}

	bucket := os.Getenv("R2_BUCKET")
	publicURL := strings.TrimRight(os.Getenv("R2_PUBLIC_URL"), "/")

	if bucket == "" || publicURL == "" {
		c.JSON(http.StatusServiceUnavailable, dto.ErrorResp{Error: "file storage not configured"})
		return
	}

	client := r2Client()
	if client == nil {
		c.JSON(http.StatusServiceUnavailable, dto.ErrorResp{Error: "file storage not configured"})
		return
	}

	// Build a scoped object key: stores/<uuid>/<original-filename>
	ext := filepath.Ext(req.Filename)
	key := fmt.Sprintf("stores/%s%s", uuid.New().String(), ext)

	presigner := s3.NewPresignClient(client)
	presigned, err := presigner.PresignPutObject(context.Background(),
		&s3.PutObjectInput{
			Bucket:        aws.String(bucket),
			Key:           aws.String(key),
			ContentType:   aws.String(req.ContentType),
			ContentLength: aws.Int64(req.Size),
		},
		s3.WithPresignExpires(15*time.Minute),
	)
	if err != nil {
		h.writeError(c, fmt.Errorf("presign: %w", err))
		return
	}

	c.JSON(http.StatusOK, dto.PresignUploadResp{
		UploadURL: presigned.URL,
		PublicURL: fmt.Sprintf("%s/%s", publicURL, key),
		Key:       key,
		ExpiresIn: 900,
	})
}
