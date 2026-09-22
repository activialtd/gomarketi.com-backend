package handler

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Cloudflare R2 is S3-compatible, so uploads use presigned PUTs: the browser
// sends the file straight to R2 and the public URL is known in advance, which
// is simpler than a signed multipart POST.
//
// R2 is used when CLOUDFLARE_R2_* is configured and Cloudinary is not.

// r2Config reads R2 credentials. Required: CLOUDFLARE_ACCOUNT_ID,
// CLOUDFLARE_R2_ACCESS_KEY_ID, CLOUDFLARE_R2_SECRET_ACCESS_KEY,
// CLOUDFLARE_R2_BUCKET. CLOUDFLARE_R2_PUBLIC_URL is the public bucket domain.
func r2Config() (accountID, accessKey, secretKey, bucket, publicBase string, ok bool) {
	accountID = getenv("CLOUDFLARE_ACCOUNT_ID", "")
	accessKey = getenv("CLOUDFLARE_R2_ACCESS_KEY_ID", "")
	secretKey = getenv("CLOUDFLARE_R2_SECRET_ACCESS_KEY", "")
	bucket = getenv("CLOUDFLARE_R2_BUCKET", "")
	publicBase = trimTrailingSlash(getenv("CLOUDFLARE_R2_PUBLIC_URL", ""))
	ok = accountID != "" && accessKey != "" && secretKey != "" && bucket != ""
	return
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// newR2Client points the S3 client at the account's R2 endpoint. R2 ignores
// regions but the SDK requires one, hence "auto".
func newR2Client(ctx context.Context, accountID, accessKey, secretKey string) (*s3.Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("r2: load config: %w", err)
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID))
	}), nil
}

// presignR2Put returns a PUT URL the browser uploads to, plus the public URL
// the file will have once uploaded.
func presignR2Put(ctx context.Context, key, contentType string) (uploadURL, publicURL string, err error) {
	accountID, accessKey, secretKey, bucket, publicBase, ok := r2Config()
	if !ok {
		return "", "", fmt.Errorf("r2 not configured")
	}

	client, err := newR2Client(ctx, accountID, accessKey, secretKey)
	if err != nil {
		return "", "", err
	}

	presigned, err := s3.NewPresignClient(client).PresignPutObject(ctx,
		&s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			ContentType: aws.String(contentType),
		},
		s3.WithPresignExpires(15*time.Minute),
	)
	if err != nil {
		return "", "", fmt.Errorf("r2: presign: %w", err)
	}
	return presigned.URL, publicBase + "/" + key, nil
}

// uploadToR2 sends one object through this service, for the multipart route.
func uploadToR2(ctx context.Context, key, contentType string, body io.Reader, size int64) (string, error) {
	accountID, accessKey, secretKey, bucket, publicBase, ok := r2Config()
	if !ok {
		return "", fmt.Errorf("r2 not configured")
	}

	client, err := newR2Client(ctx, accountID, accessKey, secretKey)
	if err != nil {
		return "", err
	}

	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
		CacheControl:  aws.String("public, max-age=31536000"),
	}); err != nil {
		return "", fmt.Errorf("r2: put object: %w", err)
	}

	return publicBase + "/" + key, nil
}
