package handler

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/activialtd/gomarketi.com-backend/services/storefront/internal/dto"
)

const maxUploadSize = 5 << 20 // 5 MB

// Uploads go to Cloudinary.
//
// Two routes, both signed server-side so the API secret never reaches a
// browser:
//
//   - /uploads/presign returns a signature the client posts straight to
//     Cloudinary, so the image never travels through this service.
//   - /stores/upload takes the file itself and forwards it, for callers that
//     would rather make one request.
//
// Cloudinary signs a request by SHA-1'ing the upload parameters (alphabetical,
// joined like a query string) with the API secret appended.

// cloudinaryConfig reads Cloudinary credentials from the environment.
// Required: CLOUDINARY_CLOUD_NAME, CLOUDINARY_API_KEY, CLOUDINARY_API_SECRET.
// Optional: CLOUDINARY_FOLDER (defaults to "gomarketi").
func cloudinaryConfig() (cloudName, apiKey, apiSecret, folder string, ok bool) {
	cloudName = getenv("CLOUDINARY_CLOUD_NAME", "")
	apiKey = getenv("CLOUDINARY_API_KEY", "")
	apiSecret = getenv("CLOUDINARY_API_SECRET", "")
	folder = getenv("CLOUDINARY_FOLDER", "gomarketi")
	ok = cloudName != "" && apiKey != "" && apiSecret != ""
	return
}

// signCloudinary builds the signature Cloudinary expects: every parameter
// except file, api_key and the signature itself, sorted by key, joined as
// k=v&k=v, with the API secret appended, then SHA-1 hex.
func signCloudinary(params map[string]string, apiSecret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "file" || k == "api_key" || k == "resource_type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}
	sb.WriteString(apiSecret)

	sum := sha1.Sum([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

func cloudinaryUploadURL(cloudName string) string {
	// "image" rather than "auto": these endpoints only accept images, and
	// being explicit stops a renamed file uploading as a raw asset.
	return fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/image/upload", cloudName)
}

// UploadStoreAsset godoc
// POST /v1/storefront/stores/upload
// Uploads a store logo or hero image through this service to Cloudinary.
// Form fields: file (image), type ("logo" | "hero")
func (h *Handler) UploadStoreAsset(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

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

	key := fmt.Sprintf("%s/%s/%d%s", store.ID, assetType, time.Now().UnixMilli(), ext)

	var url string
	switch {
	case cloudinaryConfigured():
		url, err = uploadToCloudinary(c.Request.Context(), strings.TrimSuffix(key, ext), fh.Filename, f)
	case r2Configured():
		// Sniff the type from the first bytes rather than trusting the
		// extension, then rewind for the upload itself.
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		contentType := http.DetectContentType(buf[:n])
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
			c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "failed to read file"})
			return
		}
		url, err = uploadToR2(c.Request.Context(), key, contentType, f, fh.Size)
	default:
		c.JSON(http.StatusServiceUnavailable, dto.ErrorResp{Error: "file storage not configured"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, dto.ErrorResp{Error: "upload failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": url, "type": assetType})
}

// uploadToCloudinary posts one image and returns its delivery URL.
func uploadToCloudinary(ctx context.Context, publicID, filename string, body io.Reader) (string, error) {
	cloudName, apiKey, apiSecret, folder, ok := cloudinaryConfig()
	if !ok {
		return "", fmt.Errorf("storage not configured: set CLOUDINARY_CLOUD_NAME, CLOUDINARY_API_KEY, CLOUDINARY_API_SECRET")
	}

	params := map[string]string{
		"timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"public_id": publicID,
		"folder":    folder,
	}
	params["signature"] = signCloudinary(params, apiSecret)
	params["api_key"] = apiKey

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range params {
		if err := w.WriteField(k, v); err != nil {
			return "", fmt.Errorf("cloudinary: write field %s: %w", k, err)
		}
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("cloudinary: create file part: %w", err)
	}
	if _, err := io.Copy(part, body); err != nil {
		return "", fmt.Errorf("cloudinary: copy file: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("cloudinary: close writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudinaryUploadURL(cloudName), &buf)
	if err != nil {
		return "", fmt.Errorf("cloudinary: new request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cloudinary: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("cloudinary: status %d: %s", resp.StatusCode, string(raw))
	}

	var parsed struct {
		SecureURL string `json:"secure_url"`
		URL       string `json:"url"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("cloudinary: decode response: %w", err)
	}
	if parsed.SecureURL != "" {
		return parsed.SecureURL, nil
	}
	return parsed.URL, nil
}

// PresignUpload godoc
// POST /v1/storefront/uploads/presign
//
// Returns a Cloudinary signature the client posts the file to directly, so
// the image never passes through this service. The response carries the form
// fields Cloudinary requires — post them as multipart/form-data along with
// `file`, and read `secure_url` off the reply.
func (h *Handler) PresignUpload(c *gin.Context) {
	userID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req dto.PresignUploadReq
	if !h.bind(c, &req) {
		return
	}

	purpose := req.Purpose
	if purpose == "" {
		purpose = "files"
	}

	// Namespace by store so one vendor's uploads never collide with another's.
	var storeID string
	_ = h.svc.DB().QueryRowContext(c.Request.Context(),
		`SELECT id FROM stores WHERE vendor_id=$1 AND is_active=TRUE LIMIT 1`, userID,
	).Scan(&storeID)

	publicID := fmt.Sprintf("%s/%s", purpose, uuid.New().String())
	if storeID != "" {
		publicID = fmt.Sprintf("stores/%s/%s/%s", storeID, purpose, uuid.New().String())
	}

	switch {
	case cloudinaryConfigured():
		cloudName, apiKey, apiSecret, folder, _ := cloudinaryConfig()
		timestamp := time.Now().Unix()
		params := map[string]string{
			"timestamp": fmt.Sprintf("%d", timestamp),
			"public_id": publicID,
			"folder":    folder,
		}
		signature := signCloudinary(params, apiSecret)

		// Cloudinary takes a multipart POST and returns the delivery URL,
		// so public_url is not known until the upload completes.
		c.JSON(http.StatusOK, dto.PresignUploadResp{
			UploadURL: cloudinaryUploadURL(cloudName),
			Key:       publicID,
			ExpiresIn: 3600,
			Provider:  "cloudinary",
			Fields: map[string]string{
				"api_key":   apiKey,
				"timestamp": fmt.Sprintf("%d", timestamp),
				"public_id": publicID,
				"folder":    folder,
				"signature": signature,
			},
		})

	case r2Configured():
		key := publicID + filepath.Ext(req.Filename)
		uploadURL, publicURL, err := presignR2Put(c.Request.Context(), key, req.ContentType)
		if err != nil {
			h.writeError(c, err)
			return
		}
		// R2 takes a plain PUT of the file body, and the final URL is known
		// up front.
		c.JSON(http.StatusOK, dto.PresignUploadResp{
			UploadURL: uploadURL,
			PublicURL: publicURL,
			Key:       key,
			ExpiresIn: 900,
			Provider:  "r2",
		})

	default:
		c.JSON(http.StatusServiceUnavailable, dto.ErrorResp{Error: "file storage not configured"})
	}
}

func cloudinaryConfigured() bool {
	_, _, _, _, ok := cloudinaryConfig()
	return ok
}

func r2Configured() bool {
	_, _, _, _, _, ok := r2Config()
	return ok
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
