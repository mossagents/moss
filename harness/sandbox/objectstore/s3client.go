package objectstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3Config configures the S3BlobClient.
type S3Config struct {
	// Endpoint is the S3-compatible endpoint. Leave empty for AWS S3 (auto-derived from Region).
	Endpoint string
	// Region is the AWS region (e.g. "us-east-1").
	Region string
	// Bucket is the S3 bucket name.
	Bucket string
	// AccessKeyID is the AWS/S3-compatible access key.
	AccessKeyID string
	// SecretAccessKey is the AWS/S3-compatible secret key.
	SecretAccessKey string
	// RootPrefix is prepended to all object keys (optional).
	RootPrefix string
	// PathStyle forces path-style addressing (required for MinIO, Ceph, etc.).
	PathStyle bool
}

// S3BlobClient implements BlobClient using AWS Signature V4 over plain net/http.
type S3BlobClient struct {
	cfg    S3Config
	client httpDoer
}

// NewS3BlobClient creates an S3BlobClient.
// Pass a non-nil http.Client to override the default; nil uses http.DefaultClient.
func NewS3BlobClient(cfg S3Config, client *http.Client) (*S3BlobClient, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3: bucket is required")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = fmt.Sprintf("https://s3.%s.amazonakws.com", cfg.Region)
	}
	doer := httpDoer(http.DefaultClient)
	if client != nil {
		doer = client
	}
	return &S3BlobClient{cfg: cfg, client: doer}, nil
}

func (c *S3BlobClient) objectURL(key string) string {
	if c.cfg.PathStyle {
		return fmt.Sprintf("%s/%s/%s", strings.TrimRight(c.cfg.Endpoint, "/"), c.cfg.Bucket, key)
	}
	base := strings.TrimRight(c.cfg.Endpoint, "/")
	// inject bucket subdomain
	if strings.Contains(base, "://") {
		parts := strings.SplitN(base, "://", 2)
		return fmt.Sprintf("%s://%s.%s/%s", parts[0], c.cfg.Bucket, parts[1], key)
	}
	return fmt.Sprintf("%s/%s/%s", base, c.cfg.Bucket, key)
}

func (c *S3BlobClient) fullKey(key string) string {
	if c.cfg.RootPrefix == "" {
		return key
	}
	return strings.TrimRight(c.cfg.RootPrefix, "/") + "/" + strings.TrimLeft(key, "/")
}

func (c *S3BlobClient) Get(ctx context.Context, key string) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.fullKey(key), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3 get: %w", err)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("objectstore: key not found: %s", key)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("s3 get: unexpected status %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

func (c *S3BlobClient) Put(ctx context.Context, key string, body []byte) error {
	req, err := c.newRequest(ctx, http.MethodPut, c.fullKey(key), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("s3 put: %w", err)
	}
	rb, _ := readBody(resp)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("s3 put: unexpected status %d: %s", resp.StatusCode, rb)
	}
	return nil
}

func (c *S3BlobClient) Delete(ctx context.Context, key string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, c.fullKey(key), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("s3 delete: %w", err)
	}
	rb, _ := readBody(resp)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("s3 delete: unexpected status %d: %s", resp.StatusCode, rb)
	}
	return nil
}

// s3ListBucketResult is the XML response from S3 ListObjects.
type s3ListBucketResult struct {
	XMLName     xml.Name   `xml:"ListBucketResult"`
	Contents    []s3Object `xml:"Contents"`
	IsTruncated bool       `xml:"IsTruncated"`
}

type s3Object struct {
	Key          string    `xml:"Key"`
	LastModified time.Time `xml:"LastModified"`
	ETag         string    `xml:"ETag"`
	Size         int64     `xml:"Size"`
}

func (c *S3BlobClient) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := c.fullKey(prefix)
	rawURL := c.objectURL("")
	// Strip trailing key from objectURL (we need the bucket root)
	rawURL = strings.TrimRight(rawURL, "/") + "/"

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("list-type", "2")
	if fullPrefix != "" {
		q.Set("prefix", fullPrefix)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	c.signRequest(req, nil)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3 list: %w", err)
	}
	body, _ := readBody(resp)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("s3 list: status %d: %s", resp.StatusCode, body)
	}

	var result s3ListBucketResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("s3 list: parse xml: %w", err)
	}

	// Strip root prefix from returned keys
	keys := make([]string, 0, len(result.Contents))
	root := c.cfg.RootPrefix
	if root != "" && !strings.HasSuffix(root, "/") {
		root += "/"
	}
	for _, obj := range result.Contents {
		k := obj.Key
		if root != "" {
			k = strings.TrimPrefix(k, root)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (c *S3BlobClient) Head(ctx context.Context, key string) (BlobMeta, error) {
	req, err := c.newRequest(ctx, http.MethodHead, c.fullKey(key), nil)
	if err != nil {
		return BlobMeta{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return BlobMeta{}, fmt.Errorf("s3 head: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return BlobMeta{}, fmt.Errorf("objectstore: key not found: %s", key)
	}
	if resp.StatusCode != http.StatusOK {
		return BlobMeta{}, fmt.Errorf("s3 head: status %d", resp.StatusCode)
	}
	mod, _ := http.ParseTime(resp.Header.Get("Last-Modified"))
	return BlobMeta{
		Size:         resp.ContentLength,
		ContentType:  resp.Header.Get("Content-Type"),
		LastModified: mod,
		ETag:         strings.Trim(resp.Header.Get("ETag"), `"`),
	}, nil
}

// ---- AWS Signature V4 ---------------------------------------------------

func (c *S3BlobClient) newRequest(ctx context.Context, method, key string, body []byte) (*http.Request, error) {
	rawURL := c.objectURL(key)
	var reqBody *bytes.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reqBody)
	if err != nil {
		return nil, err
	}
	c.signRequest(req, body)
	return req, nil
}

func (c *S3BlobClient) signRequest(req *http.Request, body []byte) {
	now := time.Now().UTC()
	dateShort := now.Format("20060102")
	dateLong := now.Format("20060102T150405Z")

	// Compute payload hash
	payloadHash := sha256Hex(body)

	req.Header.Set("x-amz-date", dateLong)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("Host", req.URL.Host)

	// Canonical headers (sorted)
	signedHeaders, canonicalHeaders := buildCanonicalHeaders(req)

	// Canonical request
	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := canonicalQueryString(req.URL.Query())

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// String to sign
	credentialScope := strings.Join([]string{dateShort, c.cfg.Region, "s3", "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		dateLong,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// Signing key
	signingKey := hmacSHA256(
		hmacSHA256(
			hmacSHA256(
				hmacSHA256([]byte("AWS4"+c.cfg.SecretAccessKey), []byte(dateShort)),
				[]byte(c.cfg.Region),
			),
			[]byte("s3"),
		),
		[]byte("aws4_request"),
	)

	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.cfg.AccessKeyID, credentialScope, signedHeaders, signature,
	)
	req.Header.Set("Authorization", authHeader)
}

func buildCanonicalHeaders(req *http.Request) (signedHeaders, canonicalHeaders string) {
	type kv struct{ k, v string }
	var pairs []kv
	for key, vals := range req.Header {
		lk := strings.ToLower(key)
		pairs = append(pairs, kv{lk, strings.Join(vals, ",")})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })

	var hNames, hPairs []string
	for _, p := range pairs {
		hNames = append(hNames, p.k)
		hPairs = append(hPairs, p.k+":"+strings.TrimSpace(p.v))
	}
	return strings.Join(hNames, ";"), strings.Join(hPairs, "\n") + "\n"
}

func canonicalQueryString(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vals := q[k]
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
