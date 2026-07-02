package handler

import (
	"context"
	"fmt"
	"io"
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

const (
	supabaseEndpoint   = "https://bbwhtjkuulnumskupeul.storage.supabase.co/storage/v1/s3"
	supabaseRegion     = "eu-west-1"
	supabaseBucket     = "gomarket-storefronts"
	supabasePublicBase = "https://bbwhtjkuulnumskupeul.supabase.co/storage/v1/object/public/gomarket-storefronts"
	maxUploadSize      = 5 << 20 // 5 MB
)

// UploadStoreAsset godoc
// POST /v1/storefront/stores/upload
// Uploads a store logo or hero image directly to Supabase Storage.
// Form fields: file (image), type ("logo" | "hero")
func (h *Handler) UploadStoreAsset(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	// Resolve the vendor's store to namespace the upload path.
	store, err := h.svc.GetMyStore(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	assetType := c.PostForm("type")
	if assetType != "logo" && assetType != "hero" {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "type must be 'logo' or 'hero'"})
		return
	}

	if err := c.Request.ParseMultipartForm(maxUploadSize); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "file too large (max 5MB)"})
		return
	}

	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "file is required"})
		return
	}
	if fh.Size > maxUploadSize {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "file too large (max 5MB)"})
		return
	}

	ext := strings.ToLower(filepath.Ext(fh.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		c.JSON(http.StatusBadRequest, dto.ErrorResp{Error: "only jpg, png, webp images allowed"})
		return
	}

	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "failed to read file"})
		return
	}
	defer f.Close()

	// Sniff MIME type from the first 512 bytes then rewind.
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	contentType := http.DetectContentType(buf[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "failed to read file"})
		return
	}

	// Path: {storeID}/{type}/{timestamp}{ext}
	key := fmt.Sprintf("%s/%s/%d%s", store.ID, assetType, time.Now().UnixMilli(), ext)

	if err := uploadToSupabase(c.Request.Context(), key, contentType, f, fh.Size); err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "upload failed: " + err.Error()})
		return
	}

	publicURL := supabasePublicBase + "/" + key
	c.JSON(http.StatusOK, gin.H{"url": publicURL, "type": assetType})
}

func uploadToSupabase(ctx context.Context, key, contentType string, body io.Reader, size int64) error {
	accessKeyID := envOrDefault("SUPABASE_S3_ACCESS_KEY_ID", "2002cd992364cde93f00389dc99fd422")
	secretKey := envOrDefault("SUPABASE_S3_SECRET_ACCESS_KEY", "c4325e5c4d1ab55ea1dbdb0310a2a4ee66be0269d4bba06274af6aca27198d32")

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(supabaseRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretKey, "")),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: supabaseEndpoint, HostnameImmutable: true}, nil
			},
		)),
	)
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true // Supabase requires path-style URLs
	})

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(supabaseBucket),
		Key:           aws.String(key),
		Body:          body,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
		CacheControl:  aws.String("public, max-age=31536000"),
	})
	return err
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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

	// Build a store-scoped key for multi-tenant isolation
	purpose := req.Purpose
	if purpose == "" {
		purpose = "files"
	}
	ext := filepath.Ext(req.Filename)

	// Look up the vendor's store to namespace uploads correctly
	userID, ok := h.callerID(c)
	if !ok {
		return
	}
	var storeID string
	_ = h.svc.DB().QueryRowContext(context.Background(), `SELECT id FROM stores WHERE vendor_id=$1 AND is_active=TRUE LIMIT 1`, userID).Scan(&storeID)

	var key string
	if storeID != "" {
		key = fmt.Sprintf("stores/%s/%s/%s%s", storeID, purpose, uuid.New().String(), ext)
	} else {
		key = fmt.Sprintf("uploads/%s/%s%s", purpose, uuid.New().String(), ext)
	}

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
