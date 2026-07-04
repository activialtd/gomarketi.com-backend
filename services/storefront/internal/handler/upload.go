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

const maxUploadSize = 5 << 20 // 5 MB

// supabaseS3Config reads Supabase Storage config from environment variables.
// Required: SUPABASE_S3_ENDPOINT, SUPABASE_S3_BUCKET, SUPABASE_S3_ACCESS_KEY_ID, SUPABASE_S3_SECRET_ACCESS_KEY
// Optional: SUPABASE_S3_REGION (defaults to "eu-west-1"), SUPABASE_PUBLIC_URL
func supabaseS3Config() (endpoint, region, bucket, publicBase, accessKey, secretKey string, ok bool) {
	endpoint   = os.Getenv("SUPABASE_S3_ENDPOINT")
	region     = os.Getenv("SUPABASE_S3_REGION")
	bucket     = os.Getenv("SUPABASE_S3_BUCKET")
	publicBase = strings.TrimRight(os.Getenv("SUPABASE_PUBLIC_URL"), "/")
	accessKey  = os.Getenv("SUPABASE_S3_ACCESS_KEY_ID")
	secretKey  = os.Getenv("SUPABASE_S3_SECRET_ACCESS_KEY")
	if region == "" {
		region = "eu-west-1"
	}
	ok = endpoint != "" && bucket != "" && accessKey != "" && secretKey != ""
	return
}

// newSupabaseClient builds an S3-compatible client pointed at Supabase Storage.
func newSupabaseClient(ctx context.Context, endpoint, region, accessKey, secretKey string) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: endpoint, HostnameImmutable: true}, nil
			},
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true // Supabase requires path-style URLs
	}), nil
}

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

	publicURL, err := uploadToSupabase(c.Request.Context(), key, contentType, f, fh.Size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "upload failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": publicURL, "type": assetType})
}

func uploadToSupabase(ctx context.Context, key, contentType string, body io.Reader, size int64) (string, error) {
	endpoint, region, bucket, publicBase, accessKey, secretKey, ok := supabaseS3Config()
	if !ok {
		return "", fmt.Errorf("storage not configured: set SUPABASE_S3_ENDPOINT, SUPABASE_S3_BUCKET, SUPABASE_S3_ACCESS_KEY_ID, SUPABASE_S3_SECRET_ACCESS_KEY")
	}

	client, err := newSupabaseClient(ctx, endpoint, region, accessKey, secretKey)
	if err != nil {
		return "", err
	}

	if _, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
		CacheControl:  aws.String("public, max-age=31536000"),
	}); err != nil {
		return "", err
	}

	return publicBase + "/" + key, nil
}

// PresignUpload godoc
// POST /v1/storefront/uploads/presign
// Returns a presigned PUT URL so the client can upload directly to Supabase Storage.
func (h *Handler) PresignUpload(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.PresignUploadReq
	if !h.bind(c, &req) {
		return
	}

	endpoint, region, bucket, publicBase, accessKey, secretKey, configured := supabaseS3Config()
	if !configured {
		c.JSON(http.StatusServiceUnavailable, dto.ErrorResp{Error: "file storage not configured"})
		return
	}

	client, err := newSupabaseClient(c.Request.Context(), endpoint, region, accessKey, secretKey)
	if err != nil {
		h.writeError(c, err)
		return
	}

	purpose := req.Purpose
	if purpose == "" {
		purpose = "files"
	}
	ext := filepath.Ext(req.Filename)

	var storeID string
	_ = h.svc.DB().QueryRowContext(c.Request.Context(),
		`SELECT id FROM stores WHERE vendor_id=$1 AND is_active=TRUE LIMIT 1`, userID,
	).Scan(&storeID)

	var key string
	if storeID != "" {
		key = fmt.Sprintf("stores/%s/%s/%s%s", storeID, purpose, uuid.New().String(), ext)
	} else {
		key = fmt.Sprintf("uploads/%s/%s%s", purpose, uuid.New().String(), ext)
	}

	presigner := s3.NewPresignClient(client)
	presigned, err := presigner.PresignPutObject(c.Request.Context(),
		&s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			ContentType: aws.String(req.ContentType),
		},
		s3.WithPresignExpires(15*time.Minute),
	)
	if err != nil {
		h.writeError(c, fmt.Errorf("presign: %w", err))
		return
	}

	c.JSON(http.StatusOK, dto.PresignUploadResp{
		UploadURL: presigned.URL,
		PublicURL: fmt.Sprintf("%s/%s", publicBase, key),
		Key:       key,
		ExpiresIn: 900,
	})
}
