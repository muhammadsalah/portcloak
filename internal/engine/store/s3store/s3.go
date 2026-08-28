// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package s3store stores snapshots in an S3-compatible bucket under a prefix.
//
// MinIO and AWS travel the same code path — an overridable endpoint and
// path-style addressing are configuration, not a special case — which is what
// makes the contract suite a compatibility statement rather than a claim.
package s3store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/store"
)

// DefaultPartSize is the multipart chunk. Smaller parts resume faster on a
// flaky link, which is why it is tunable per storage definition.
const DefaultPartSize = 8 << 20

// Store is the S3 ResumableStore.
type Store struct {
	name     string
	bucket   string
	prefix   string
	endpoint string
	partSize int64
	class    types.StorageClass
	sse      types.ServerSideEncryption

	cli *s3.Client
}

// Credential is what an operator supplied for an S3 storage. It is stored in
// the OS keychain as a single value and split here.
type Credential struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken,omitempty"`
}

// ParseCredential reads the keychain value, accepting either JSON or the
// `key:secret` shorthand an operator is likely to paste.
func ParseCredential(raw string) (Credential, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Credential{}, nil
	}
	if strings.HasPrefix(raw, "{") {
		var c Credential
		if err := jsonUnmarshal([]byte(raw), &c); err != nil {
			return Credential{}, resil.Fatal("read the S3 credential",
				"The stored credential is not readable as JSON.", err)
		}
		return c, nil
	}
	id, secret, ok := strings.Cut(raw, ":")
	if !ok {
		return Credential{}, resil.Fatal("read the S3 credential",
			"The stored credential should be an access key and secret, separated by a colon.", nil)
	}
	return Credential{AccessKeyID: strings.TrimSpace(id), SecretAccessKey: strings.TrimSpace(secret)}, nil
}

// New builds an S3 store from a storage definition.
func New(ctx context.Context, st config.Storage, creds config.CredentialStore) (*Store, error) {
	raw, err := config.Resolve(creds, st.CredentialRef, st.Name)
	if err != nil {
		return nil, err
	}
	cred, err := ParseCredential(raw)
	if err != nil {
		return nil, err
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if st.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(st.Region))
	} else {
		// A MinIO endpoint needs a region even though it ignores it.
		loadOpts = append(loadOpts, awsconfig.WithRegion("us-east-1"))
	}
	if cred.AccessKeyID != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cred.AccessKeyID, cred.SecretAccessKey, cred.SessionToken)))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, resil.Fatal("configure the S3 client",
			"PortCloak could not build an S3 client from this storage definition.", err)
	}

	cli := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if st.Endpoint != "" {
			o.BaseEndpoint = aws.String(normaliseEndpoint(st.Endpoint))
		}
		// Path-style is what makes MinIO and other S3-compatible stores work
		// without DNS games.
		o.UsePathStyle = st.PathStyle
	})

	partSize := int64(st.PartSizeMB) << 20
	if partSize < 5<<20 {
		partSize = DefaultPartSize
	}

	return &Store{
		name:     st.Name,
		bucket:   st.Bucket,
		prefix:   strings.Trim(st.Prefix, "/"),
		endpoint: st.Endpoint,
		partSize: partSize,
		class:    types.StorageClass(st.StorageClass),
		sse:      types.ServerSideEncryption(st.ServerSideEnc),
		cli:      cli,
	}, nil
}

func normaliseEndpoint(e string) string {
	if strings.HasPrefix(e, "http://") || strings.HasPrefix(e, "https://") {
		return e
	}
	return "https://" + e
}

// Endpoint identifies this store for the circuit breaker and error messages.
func (s *Store) Endpoint() string {
	if s.endpoint != "" {
		return fmt.Sprintf("s3://%s/%s @ %s", s.bucket, s.prefix, s.endpoint)
	}
	return fmt.Sprintf("s3://%s/%s", s.bucket, s.prefix)
}

// Close releases nothing; the SDK client is stateless.
func (s *Store) Close() error { return nil }

// PartSize is the chunk this store uploads in.
func (s *Store) PartSize() int64 { return s.partSize }

func (s *Store) key(k string) string {
	if s.prefix == "" {
		return k
	}
	return s.prefix + "/" + k
}

func (s *Store) unkey(k string) string {
	if s.prefix == "" {
		return k
	}
	return strings.TrimPrefix(strings.TrimPrefix(k, s.prefix), "/")
}

// Probe performs the round trip UC-S3 describes: list the prefix, do a small
// multipart upload, verify it, then remove the probe object.
// EnsureBucket creates the bucket, for the "it does not exist, shall I create
// it?" path.
//
// BucketAlreadyOwnedByYou is success: the bucket is there and it is this
// account's. BucketAlreadyExists is not, and is deliberately not folded in with
// it — that code means the name belongs to somebody else, and treating it as
// success would leave PortCloak pointed at a bucket nobody here can write to.
func (s *Store) EnsureBucket(ctx context.Context) error {
	in := &s3.CreateBucketInput{Bucket: aws.String(s.bucket)}
	// us-east-1 is the API's own default and is rejected when named explicitly,
	// so it is the one region that must be left unsaid.
	if region := s.cli.Options().Region; region != "" && region != "us-east-1" {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	if _, err := s.cli.CreateBucket(ctx, in); err != nil {
		var owned *types.BucketAlreadyOwnedByYou
		if errors.As(err, &owned) {
			return nil
		}
		return classify(err)
	}
	return nil
}

func (s *Store) Probe(ctx context.Context) (store.Reach, error) {
	start := time.Now()
	r := store.Reach{
		Root:      s.Endpoint(),
		Resumable: true,
		Integrity: store.IntegrityServerSide,
		ProbedAt:  start,
	}

	if _, err := s.cli.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket), Prefix: aws.String(s.prefix), MaxKeys: aws.Int32(1),
	}); err != nil {
		// A bucket that is not there yet is created rather than reported as a
		// failure. Only its absence is treated that way: a rejected credential
		// or a denied listing is the operator's to see, and must not arrive
		// dressed as a bucket that simply had not been made.
		var api smithy.APIError
		if !errors.As(err, &api) || api.ErrorCode() != "NoSuchBucket" {
			r.Access = store.AccessNone
			r.FailedStep = "ListObjects"
			r.Detail = describeAWSError(err, s.bucket)
			r.Latency = time.Since(start)
			return r, nil
		}
		if mkErr := s.EnsureBucket(ctx); mkErr != nil {
			r.Access = store.AccessNone
			r.FailedStep = "creating the bucket"
			r.Detail = fmt.Sprintf("There is no bucket called %q at this endpoint and it could not be created: %v", s.bucket, mkErr)
			r.Latency = time.Since(start)
			return r, nil
		}
	}
	r.Access = store.AccessReadOnly

	probeKey := s.key(".portcloak-probe")
	payload := []byte("portcloak write probe")
	sum := sha256.Sum256(payload)

	// The abort is deferred before the upload starts, so an interrupted probe
	// cannot leave orphan parts accruing cost.
	created, err := s.cli.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(probeKey),
	})
	if err != nil {
		r.Detail = "The prefix can be listed but not written to: " + describeAWSError(err, s.bucket)
		r.Latency = time.Since(start)
		return r, nil
	}
	uploadID := created.UploadId
	committed := false
	defer func() {
		if !committed {
			_, _ = s.cli.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
				Bucket: aws.String(s.bucket), Key: aws.String(probeKey), UploadId: uploadID,
			})
		}
		_, _ = s.cli.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket), Key: aws.String(probeKey),
		})
	}()

	part, err := s.cli.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(s.bucket), Key: aws.String(probeKey),
		UploadId: uploadID, PartNumber: aws.Int32(1),
		Body: strings.NewReader(string(payload)),
	})
	if err != nil {
		r.FailedStep = "UploadPart"
		r.Detail = describeAWSError(err, s.bucket)
		r.Latency = time.Since(start)
		return r, nil
	}
	if _, err := s.cli.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(probeKey), UploadId: uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: []types.CompletedPart{{ETag: part.ETag, PartNumber: aws.Int32(1)}},
		},
	}); err != nil {
		r.FailedStep = "CompleteMultipartUpload"
		r.Detail = describeAWSError(err, s.bucket)
		r.Latency = time.Since(start)
		return r, nil
	}
	committed = true

	var back strings.Builder
	if _, err := s.Get(ctx, ".portcloak-probe", &back, store.GetOptions{}); err != nil || back.String() != string(payload) {
		r.FailedStep = "reading the probe object back"
		r.Detail = "The bucket accepted a write but did not return the same bytes."
		r.Latency = time.Since(start)
		return r, nil
	}
	backSum := sha256.Sum256([]byte(back.String()))
	if backSum != sum {
		r.FailedStep = "verifying the probe object"
		r.Detail = "The bucket returned different bytes than were written."
		return r, nil
	}

	r.Access = store.AccessWritable
	r.Latency = time.Since(start)
	return r, nil
}

// describeAWSError names the operation and the likely cause rather than
// wrapping an SDK error.
func describeAWSError(err error, bucket string) string {
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchBucket":
			return fmt.Sprintf("There is no bucket called %q at this endpoint.", bucket)
		case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
			return "The credentials were rejected. " + clockSkewNote(err)
		case "RequestTimeTooSkewed":
			return "This machine's clock is too far from the server's for a request signature to be accepted."
		}
		return fmt.Sprintf("%s: %s", api.ErrorCode(), api.ErrorMessage())
	}
	return err.Error()
}

func clockSkewNote(err error) string {
	if strings.Contains(strings.ToLower(err.Error()), "skew") {
		return "This machine's clock may be too far from the server's."
	}
	return "Check the access key and secret, and that they have access to this bucket and prefix."
}

// classify maps an S3 failure onto a retry decision.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchBucket", "NoSuchKey", "AccessDenied", "InvalidAccessKeyId",
			"SignatureDoesNotMatch", "InvalidArgument", "EntityTooSmall", "NoSuchUpload":
			return resil.Fatal("talk to S3", describeAWSError(err, ""), err)
		case "SlowDown", "RequestTimeout", "InternalError", "ServiceUnavailable":
			return resil.Retry("talk to S3", "The bucket asked PortCloak to slow down or was briefly unavailable.", err)
		}
	}
	var httpErr *smithyhttp.ResponseError
	if errors.As(err, &httpErr) {
		if resil.ClassifyHTTPStatus(httpErr.HTTPStatusCode()) == resil.Retryable {
			return resil.Retry("talk to S3",
				fmt.Sprintf("The bucket returned %d.", httpErr.HTTPStatusCode()), err)
		}
		return resil.Fatal("talk to S3",
			fmt.Sprintf("The bucket returned %d.", httpErr.HTTPStatusCode()), err)
	}
	if resil.ClassifyNetwork(err) == resil.Retryable {
		return resil.Retry("talk to S3", "The connection to the bucket dropped.", err)
	}
	return resil.Fatal("talk to S3", err.Error(), err)
}

func isNotFound(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var httpErr *smithyhttp.ResponseError
	if errors.As(err, &httpErr) && httpErr.HTTPStatusCode() == http.StatusNotFound {
		return true
	}
	return false
}

// Stat reports on one object.
func (s *Store) Stat(ctx context.Context, key string) (store.ObjectInfo, error) {
	out, err := s.cli.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
	})
	if err != nil {
		if isNotFound(err) {
			return store.ObjectInfo{}, fmt.Errorf("%w: %s", store.ErrNotFound, key)
		}
		return store.ObjectInfo{}, classify(err)
	}
	info := store.ObjectInfo{Key: key}
	if out.ContentLength != nil {
		info.Size = *out.ContentLength
	}
	if out.LastModified != nil {
		info.ModTime = *out.LastModified
	}
	if out.ETag != nil {
		info.ETag = strings.Trim(*out.ETag, `"`)
	}
	return info, nil
}

// Put writes an object, using multipart above the part threshold.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, opts store.PutOptions) (store.PutResult, error) {
	if opts.Size > 0 && opts.Size <= s.partSize {
		return s.putSingle(ctx, key, r, opts)
	}
	return s.putMultipart(ctx, key, r, opts)
}

func (s *Store) putSingle(ctx context.Context, key string, r io.Reader, opts store.PutOptions) (store.PutResult, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return store.PutResult{}, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if opts.Digest != "" && opts.Digest != digest {
		return store.PutResult{}, resil.Fatal("verify the upload",
			fmt.Sprintf("%s did not arrive intact — the bytes do not match the digest computed before the transfer.", key), nil)
	}

	in := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
		Body:          strings.NewReader(string(body)),
		ContentLength: aws.Int64(int64(len(body))),
		// The server-side checksum is what lets the client-side digest be
		// cross-checked rather than merely trusted.
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	}
	if s.class != "" {
		in.StorageClass = s.class
	}
	if s.sse != "" {
		in.ServerSideEncryption = s.sse
	}
	out, err := s.cli.PutObject(ctx, in)
	if err != nil {
		return store.PutResult{}, classify(err)
	}
	if opts.Progress != nil {
		opts.Progress(int64(len(body)))
	}
	res := store.PutResult{Key: key, Size: int64(len(body)), Digest: digest}
	if out.ETag != nil {
		res.ETag = strings.Trim(*out.ETag, `"`)
	}
	return res, nil
}

func (s *Store) putMultipart(ctx context.Context, key string, r io.Reader, opts store.PutOptions) (store.PutResult, error) {
	id, err := s.InitMultipart(ctx, key)
	if err != nil {
		return store.PutResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			// A give-up aborts rather than leaving orphan parts to accrue cost.
			_ = s.AbortMultipart(context.WithoutCancel(ctx), id, key)
		}
	}()

	var parts []store.PartETag
	var written int64
	h := sha256.New()
	buf := make([]byte, s.partSize)

	for n := 1; ; n++ {
		read, readErr := io.ReadFull(r, buf)
		if read > 0 {
			h.Write(buf[:read])
			p, err := s.PutPart(ctx, id, key, n, strings.NewReader(string(buf[:read])), int64(read))
			if err != nil {
				return store.PutResult{}, err
			}
			parts = append(parts, p)
			written += int64(read)
			if opts.Progress != nil {
				opts.Progress(written)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
				break
			}
			return store.PutResult{}, readErr
		}
	}

	// A stream that turned out to be empty cannot be completed as a multipart
	// upload: S3 and MinIO both reject CompleteMultipartUpload without at
	// least one part, and the caller gets a bare 400.
	//
	// This path is reached whenever the size was not known in advance, because
	// Put routes on `opts.Size > 0` and cannot tell "empty" from "unknown".
	// The objects that arrive this way are real — an empty realm's user file,
	// or a sidecar an interrupted job never filled in — so the object is
	// written as a single zero-byte Put instead. The deferred abort disposes
	// of the upload this function opened.
	if len(parts) == 0 {
		return s.putSingle(ctx, key, strings.NewReader(""), opts)
	}

	digest := hex.EncodeToString(h.Sum(nil))
	if opts.Digest != "" && opts.Offset == 0 && opts.Digest != digest {
		return store.PutResult{}, resil.Fatal("verify the upload",
			fmt.Sprintf("%s did not arrive intact — the bytes do not match the digest computed before the transfer.", key), nil)
	}

	res, err := s.CompleteMultipart(ctx, id, key, parts)
	if err != nil {
		return store.PutResult{}, err
	}
	committed = true
	res.Digest = digest
	if opts.Digest != "" {
		res.Digest = opts.Digest
	}
	return res, nil
}

// InitMultipart starts a resumable upload.
func (s *Store) InitMultipart(ctx context.Context, key string) (store.UploadID, error) {
	in := &s3.CreateMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key))}
	if s.class != "" {
		in.StorageClass = s.class
	}
	if s.sse != "" {
		in.ServerSideEncryption = s.sse
	}
	out, err := s.cli.CreateMultipartUpload(ctx, in)
	if err != nil {
		return "", classify(err)
	}
	return store.UploadID(aws.ToString(out.UploadId)), nil
}

// PutPart uploads one part.
func (s *Store) PutPart(ctx context.Context, id store.UploadID, key string, number int, r io.Reader, size int64) (store.PartETag, error) {
	out, err := s.cli.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
		UploadId: aws.String(string(id)), PartNumber: aws.Int32(int32(number)),
		Body: r, ContentLength: aws.Int64(size),
	})
	if err != nil {
		return store.PartETag{}, classify(err)
	}
	return store.PartETag{Number: number, ETag: strings.Trim(aws.ToString(out.ETag), `"`), Size: size}, nil
}

// CompleteMultipart finishes an upload.
func (s *Store) CompleteMultipart(ctx context.Context, id store.UploadID, key string, parts []store.PartETag) (store.PutResult, error) {
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	completed := make([]types.CompletedPart, 0, len(parts))
	var size int64
	for _, p := range parts {
		completed = append(completed, types.CompletedPart{
			ETag: aws.String(p.ETag), PartNumber: aws.Int32(int32(p.Number)),
		})
		size += p.Size
	}

	out, err := s.cli.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
		UploadId:        aws.String(string(id)),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	})
	if err != nil {
		return store.PutResult{}, classify(err)
	}
	return store.PutResult{Key: key, Size: size, ETag: strings.Trim(aws.ToString(out.ETag), `"`)}, nil
}

// AbortMultipart cleans up, so a cancelled or discarded job does not leave
// billable incomplete uploads behind.
func (s *Store) AbortMultipart(ctx context.Context, id store.UploadID, key string) error {
	_, err := s.cli.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)), UploadId: aws.String(string(id)),
	})
	if err != nil && !isNotFound(err) {
		return classify(err)
	}
	return nil
}

// ListParts re-establishes what the server already holds.
//
// This is what makes resume work after PortCloak itself has been restarted: the
// checkpoint carries the upload id, and the server is the authority on which
// parts actually landed.
func (s *Store) ListParts(ctx context.Context, id store.UploadID, key string) ([]store.PartETag, error) {
	var out []store.PartETag
	var marker *string
	for {
		page, err := s.cli.ListParts(ctx, &s3.ListPartsInput{
			Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
			UploadId: aws.String(string(id)), PartNumberMarker: marker,
		})
		if err != nil {
			return nil, classify(err)
		}
		for _, p := range page.Parts {
			out = append(out, store.PartETag{
				Number: int(aws.ToInt32(p.PartNumber)),
				ETag:   strings.Trim(aws.ToString(p.ETag), `"`),
				Size:   aws.ToInt64(p.Size),
			})
		}
		if !aws.ToBool(page.IsTruncated) {
			break
		}
		marker = page.NextPartNumberMarker
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// Get reads an object.
func (s *Store) Get(ctx context.Context, key string, w io.Writer, opts store.GetOptions) (store.GetResult, error) {
	in := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key))}
	if opts.Offset > 0 {
		in.Range = aws.String(fmt.Sprintf("bytes=%d-", opts.Offset))
	}
	out, err := s.cli.GetObject(ctx, in)
	if err != nil {
		if isNotFound(err) {
			return store.GetResult{}, fmt.Errorf("%w: %s", store.ErrNotFound, key)
		}
		return store.GetResult{}, classify(err)
	}
	defer out.Body.Close() //nolint:errcheck

	h := sha256.New()
	pw := &store.ProgressWriter{W: io.MultiWriter(w, h), Ctx: ctx, OnWrite: opts.Progress, Written: opts.Offset}
	n, err := io.Copy(pw, out.Body)
	if err != nil {
		return store.GetResult{}, resil.Retry("download the snapshot",
			"The connection to the bucket dropped partway through the download.", err)
	}
	return store.GetResult{Size: opts.Offset + n, Digest: hex.EncodeToString(h.Sum(nil))}, nil
}

// List returns every object under a prefix, paging so a bucket with hundreds of
// snapshots lists correctly rather than truncating at the first page.
func (s *Store) List(ctx context.Context, prefix string) ([]store.ObjectInfo, error) {
	full := s.key(prefix)
	var out []store.ObjectInfo
	var token *string

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := s.cli.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket), Prefix: aws.String(full), ContinuationToken: token,
		})
		if err != nil {
			return nil, classify(err)
		}
		for _, o := range page.Contents {
			key := s.unkey(aws.ToString(o.Key))
			if strings.HasSuffix(key, ".portcloak-probe") {
				continue
			}
			info := store.ObjectInfo{Key: key, Size: aws.ToInt64(o.Size)}
			if o.LastModified != nil {
				info.ModTime = *o.LastModified
			}
			if o.ETag != nil {
				info.ETag = strings.Trim(*o.ETag, `"`)
			}
			out = append(out, info)
		}
		if !aws.ToBool(page.IsTruncated) {
			break
		}
		token = page.NextContinuationToken
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Delete removes an object. Deleting one that is not there has already reached
// the desired end state.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.cli.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
	})
	if err != nil && !isNotFound(err) {
		return classify(err)
	}
	return nil
}

// jsonUnmarshal is a thin indirection so the credential parser does not pull
// encoding/json into the top of a file that is otherwise about transfers.
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
