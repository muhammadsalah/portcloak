// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package azurestore stores snapshots in an Azure Blob container under a
// prefix.
//
// Azurite and real Azure travel the same code path, pointed at different
// endpoints. That is how the Azure path is developed and tested at all, and it
// is also the honest limit of what "supported" means here: Azurite's fidelity
// is good but not total, so a real account is exercised manually before a
// release and the divergences are recorded rather than remembered.
package azurestore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/store"
)

// DefaultBlockSize is the staged block size.
const DefaultBlockSize = 8 << 20

// Store is the Azure ResumableStore.
type Store struct {
	name      string
	container string
	prefix    string
	blockSize int64
	tier      string

	client *azblob.Client
}

// New builds an Azure store from a storage definition.
//
// The credential is accepted as a connection string, an account key, or a SAS
// token, because those are the three things an operator actually has.
func New(st config.Storage, creds config.CredentialStore) (*Store, error) {
	raw, err := config.Resolve(creds, st.CredentialRef, st.Name)
	if err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(raw)

	blockSize := int64(st.BlockSizeMB) << 20
	if blockSize <= 0 {
		blockSize = DefaultBlockSize
	}

	s := &Store{
		name:      st.Name,
		container: st.Container,
		prefix:    strings.Trim(st.Prefix, "/"),
		blockSize: blockSize,
		tier:      st.AccessTier,
	}

	switch {
	case strings.Contains(raw, "AccountKey=") || strings.Contains(raw, "BlobEndpoint="):
		// A connection string is what Azurite hands out, so it is the shortest
		// path to a working emulator setup.
		client, err := azblob.NewClientFromConnectionString(raw, nil)
		if err != nil {
			return nil, resil.Fatal("configure the Azure client",
				"That connection string could not be used.", err)
		}
		s.client = client

	case strings.HasPrefix(raw, "?") || strings.Contains(raw, "sig="):
		url := serviceURL(st)
		client, err := azblob.NewClientWithNoCredential(url+strings.TrimPrefix(raw, "?"), nil)
		if err != nil {
			return nil, resil.Fatal("configure the Azure client", "That SAS token could not be used.", err)
		}
		s.client = client

	case raw != "" && st.Account != "":
		cred, err := azblob.NewSharedKeyCredential(st.Account, raw)
		if err != nil {
			return nil, resil.Fatal("configure the Azure client",
				"That account key could not be used.", err)
		}
		client, err := azblob.NewClientWithSharedKeyCredential(serviceURL(st), cred, nil)
		if err != nil {
			return nil, resil.Fatal("configure the Azure client", "PortCloak could not build an Azure client.", err)
		}
		s.client = client

	default:
		return nil, resil.Fatal("configure the Azure client",
			fmt.Sprintf("No usable credential was found for %q.", st.Name), nil).
			WithAdvice("Supply a connection string, an account key, or a SAS token.")
	}
	return s, nil
}

func serviceURL(st config.Storage) string {
	if st.Endpoint != "" {
		e := st.Endpoint
		if !strings.HasPrefix(e, "http") {
			e = "https://" + e
		}
		return strings.TrimRight(e, "/") + "/"
	}
	return fmt.Sprintf("https://%s.blob.core.windows.net/", st.Account)
}

// Endpoint identifies this store for the circuit breaker and error messages.
func (s *Store) Endpoint() string {
	return fmt.Sprintf("azure://%s/%s", s.container, s.prefix)
}

// Close releases nothing; the SDK client is stateless.
func (s *Store) Close() error { return nil }

// PartSize is the block this store uploads in.
func (s *Store) PartSize() int64 { return s.blockSize }

func (s *Store) name_(k string) string {
	if s.prefix == "" {
		return k
	}
	return s.prefix + "/" + k
}

func (s *Store) unname(k string) string {
	if s.prefix == "" {
		return k
	}
	return strings.TrimPrefix(strings.TrimPrefix(k, s.prefix), "/")
}

func (s *Store) blob(key string) *blockblob.Client {
	return s.client.ServiceClient().NewContainerClient(s.container).NewBlockBlobClient(s.name_(key))
}

// Probe performs the round trip UC-S4 describes: list the prefix, stage and
// commit a small block blob, verify it, then delete it.
func (s *Store) Probe(ctx context.Context) (store.Reach, error) {
	start := time.Now()
	r := store.Reach{
		Root:      s.Endpoint(),
		Resumable: true,
		Integrity: store.IntegrityServerSide,
		ProbedAt:  start,
	}

	pager := s.client.NewListBlobsFlatPager(s.container, &azblob.ListBlobsFlatOptions{
		Prefix: strPtr(s.prefix),
	})
	if pager.More() {
		if _, err := pager.NextPage(ctx); err != nil {
			// A container that is not there yet is created rather than
			// reported as a failure, and the listing is attempted once more
			// against it. Anything other than its absence is the operator's to
			// see: a rejected credential must not read as a missing container.
			if !bloberror.HasCode(err, bloberror.ContainerNotFound) {
				r.Access = store.AccessNone
				r.FailedStep = "ListBlobs"
				r.Detail = describeAzureError(err, s.container)
				r.Latency = time.Since(start)
				return r, nil
			}
			if mkErr := s.EnsureContainer(ctx); mkErr != nil {
				r.Access = store.AccessNone
				r.FailedStep = "creating the container"
				r.Detail = fmt.Sprintf("There is no container called %q at this endpoint and it could not be created: %v", s.container, mkErr)
				r.Latency = time.Since(start)
				return r, nil
			}
		}
	}
	r.Access = store.AccessReadOnly

	probeKey := ".portcloak-probe"
	payload := []byte("portcloak write probe")
	bc := s.blob(probeKey)

	// Cleanup is deferred before the staging starts, so uncommitted blocks from
	// an interrupted probe do not linger.
	defer func() {
		_, _ = bc.Delete(context.WithoutCancel(ctx), nil)
	}()

	blockID := encodeBlockID(1)
	if _, err := bc.StageBlock(ctx, blockID, streamOf(payload), nil); err != nil {
		r.Detail = "The container can be listed but not written to: " + describeAzureError(err, s.container)
		r.Latency = time.Since(start)
		return r, nil
	}
	if _, err := bc.CommitBlockList(ctx, []string{blockID}, nil); err != nil {
		r.FailedStep = "CommitBlockList"
		r.Detail = describeAzureError(err, s.container)
		r.Latency = time.Since(start)
		return r, nil
	}

	var back strings.Builder
	if _, err := s.Get(ctx, probeKey, &back, store.GetOptions{}); err != nil || back.String() != string(payload) {
		r.FailedStep = "reading the probe blob back"
		r.Detail = "The container accepted a write but did not return the same bytes."
		r.Latency = time.Since(start)
		return r, nil
	}

	r.Access = store.AccessWritable
	r.Latency = time.Since(start)
	return r, nil
}

// EnsureContainer creates the container, for the "it does not exist, shall I
// create it?" path.
func (s *Store) EnsureContainer(ctx context.Context) error {
	_, err := s.client.CreateContainer(ctx, s.container, nil)
	if err != nil && !bloberror.HasCode(err, bloberror.ContainerAlreadyExists) {
		return classify(err, s.container)
	}
	return nil
}

func describeAzureError(err error, containerName string) string {
	switch {
	case bloberror.HasCode(err, bloberror.ContainerNotFound):
		return fmt.Sprintf("There is no container called %q at this endpoint.", containerName)
	case bloberror.HasCode(err, bloberror.ResourceNotFound):
		// A 404 naming the resource rather than the container means the request
		// never identified a storage account, so the container was not looked
		// for at all. Azure carries the account in the host name and an
		// emulator carries it as the first path segment, which is the way an
		// endpoint that stops at the port goes wrong: the container name is
		// left sitting where the account name should be, and is read as one.
		// Reporting this as a missing container would send the operator to
		// look at the one thing that is not the problem.
		return fmt.Sprintf("No storage account was found at this endpoint, so %q was never looked up as a container. "+
			"An account is named in the host by Azure itself and as the first path segment by an emulator, "+
			"so the endpoint has to carry it rather than stopping at the host or the port.", containerName)
	case bloberror.HasCode(err, bloberror.AuthenticationFailed):
		return "The credential was rejected. Check the account key, connection string or SAS."
	case bloberror.HasCode(err, bloberror.AuthorizationPermissionMismatch):
		return "The credential is valid but does not have permission for this operation."
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		if respErr.StatusCode == 403 {
			return "The credential was refused, which for a SAS usually means it has expired."
		}
		return fmt.Sprintf("%s (HTTP %d)", respErr.ErrorCode, respErr.StatusCode)
	}
	return err.Error()
}

func classify(err error, containerName string) error {
	if err == nil {
		return nil
	}
	switch {
	case bloberror.HasCode(err, bloberror.ContainerNotFound, bloberror.BlobNotFound,
		bloberror.ResourceNotFound, bloberror.AuthenticationFailed,
		bloberror.AuthorizationPermissionMismatch, bloberror.InvalidBlockList):
		return resil.Fatal("talk to Azure Blob", describeAzureError(err, containerName), err)
	case bloberror.HasCode(err, bloberror.ServerBusy, bloberror.InternalError, bloberror.OperationTimedOut):
		return resil.Retry("talk to Azure Blob", "The container was briefly unavailable.", err)
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		if resil.ClassifyHTTPStatus(respErr.StatusCode) == resil.Retryable {
			return resil.Retry("talk to Azure Blob",
				fmt.Sprintf("The container returned %d.", respErr.StatusCode), err)
		}
		return resil.Fatal("talk to Azure Blob", describeAzureError(err, containerName), err)
	}
	if resil.ClassifyNetwork(err) == resil.Retryable {
		return resil.Retry("talk to Azure Blob", "The connection to the container dropped.", err)
	}
	return resil.Fatal("talk to Azure Blob", err.Error(), err)
}

// Stat reports on one blob.
func (s *Store) Stat(ctx context.Context, key string) (store.ObjectInfo, error) {
	props, err := s.blob(key).GetProperties(ctx, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return store.ObjectInfo{}, fmt.Errorf("%w: %s", store.ErrNotFound, key)
		}
		return store.ObjectInfo{}, classify(err, s.container)
	}
	info := store.ObjectInfo{Key: key}
	if props.ContentLength != nil {
		info.Size = *props.ContentLength
	}
	if props.LastModified != nil {
		info.ModTime = *props.LastModified
	}
	if props.ETag != nil {
		info.ETag = strings.Trim(string(*props.ETag), `"`)
	}
	return info, nil
}

// Put writes a blob as staged blocks.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, opts store.PutOptions) (store.PutResult, error) {
	bc := s.blob(key)
	h := sha256.New()
	var blockIDs []string
	var written int64
	buf := make([]byte, s.blockSize)

	committed := false
	defer func() {
		if !committed {
			// Uncommitted blocks are cleaned up so a failed upload does not
			// leave storage paying for a partial object.
			_, _ = bc.Delete(context.WithoutCancel(ctx), nil)
		}
	}()

	for n := 1; ; n++ {
		read, readErr := io.ReadFull(r, buf)
		if read > 0 {
			h.Write(buf[:read])
			id := encodeBlockID(n)
			if _, err := bc.StageBlock(ctx, id, streamOf(buf[:read]), nil); err != nil {
				return store.PutResult{}, classify(err, s.container)
			}
			blockIDs = append(blockIDs, id)
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

	digest := hex.EncodeToString(h.Sum(nil))
	if opts.Digest != "" && opts.Offset == 0 && opts.Digest != digest {
		return store.PutResult{}, resil.Fatal("verify the upload",
			fmt.Sprintf("%s did not arrive intact. The bytes do not match the digest computed before the transfer.", key), nil)
	}

	commitOpts := &blockblob.CommitBlockListOptions{}
	if s.tier != "" {
		tier := blob.AccessTier(s.tier)
		commitOpts.Tier = &tier
	}
	out, err := bc.CommitBlockList(ctx, blockIDs, commitOpts)
	if err != nil {
		return store.PutResult{}, classify(err, s.container)
	}
	committed = true

	res := store.PutResult{Key: key, Size: written, Digest: digest, Resumed: opts.Offset > 0}
	if opts.Digest != "" {
		res.Digest = opts.Digest
	}
	if out.ETag != nil {
		res.ETag = strings.Trim(string(*out.ETag), `"`)
	}
	return res, nil
}

// InitMultipart is a no-op for Azure: staged blocks belong to the blob itself,
// so the blob name is the upload identity.
func (s *Store) InitMultipart(ctx context.Context, key string) (store.UploadID, error) {
	return store.UploadID(s.name_(key)), nil
}

// PutPart stages one block.
func (s *Store) PutPart(ctx context.Context, id store.UploadID, key string, number int, r io.Reader, size int64) (store.PartETag, error) {
	body, err := io.ReadAll(io.LimitReader(r, size))
	if err != nil {
		return store.PartETag{}, err
	}
	blockID := encodeBlockID(number)
	if _, err := s.blob(key).StageBlock(ctx, blockID, streamOf(body), nil); err != nil {
		return store.PartETag{}, classify(err, s.container)
	}
	return store.PartETag{Number: number, ETag: blockID, Size: int64(len(body))}, nil
}

// CompleteMultipart commits the staged block list.
func (s *Store) CompleteMultipart(ctx context.Context, id store.UploadID, key string, parts []store.PartETag) (store.PutResult, error) {
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	ids := make([]string, 0, len(parts))
	var size int64
	for _, p := range parts {
		ids = append(ids, p.ETag)
		size += p.Size
	}
	out, err := s.blob(key).CommitBlockList(ctx, ids, nil)
	if err != nil {
		return store.PutResult{}, classify(err, s.container)
	}
	res := store.PutResult{Key: key, Size: size}
	if out.ETag != nil {
		res.ETag = strings.Trim(string(*out.ETag), `"`)
	}
	return res, nil
}

// AbortMultipart discards staged blocks by deleting the uncommitted blob.
func (s *Store) AbortMultipart(ctx context.Context, id store.UploadID, key string) error {
	_, err := s.blob(key).Delete(ctx, nil)
	if err != nil && !bloberror.HasCode(err, bloberror.BlobNotFound) {
		return classify(err, s.container)
	}
	return nil
}

// ListParts re-establishes which blocks the service already holds.
//
// Uncommitted blocks are exactly what makes Azure resumable: after a restart
// PortCloak re-stages only the missing ones and commits.
func (s *Store) ListParts(ctx context.Context, id store.UploadID, key string) ([]store.PartETag, error) {
	out, err := s.blob(key).GetBlockList(ctx, blockblob.BlockListTypeAll, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, fmt.Errorf("%w: %s", store.ErrNotFound, key)
		}
		return nil, classify(err, s.container)
	}

	var parts []store.PartETag
	collect := func(blocks []*blockblob.Block) {
		for _, b := range blocks {
			if b == nil || b.Name == nil {
				continue
			}
			n := decodeBlockID(*b.Name)
			if n == 0 {
				continue
			}
			var size int64
			if b.Size != nil {
				size = *b.Size
			}
			parts = append(parts, store.PartETag{Number: n, ETag: *b.Name, Size: size})
		}
	}
	collect(out.CommittedBlocks)
	collect(out.UncommittedBlocks)

	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	return parts, nil
}

// Get reads a blob.
func (s *Store) Get(ctx context.Context, key string, w io.Writer, opts store.GetOptions) (store.GetResult, error) {
	dl, err := s.blob(key).DownloadStream(ctx, &blob.DownloadStreamOptions{
		Range: blob.HTTPRange{Offset: opts.Offset},
	})
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return store.GetResult{}, fmt.Errorf("%w: %s", store.ErrNotFound, key)
		}
		return store.GetResult{}, classify(err, s.container)
	}
	defer dl.Body.Close() //nolint:errcheck

	h := sha256.New()
	pw := &store.ProgressWriter{W: io.MultiWriter(w, h), Ctx: ctx, OnWrite: opts.Progress, Written: opts.Offset}
	n, err := io.Copy(pw, dl.Body)
	if err != nil {
		return store.GetResult{}, resil.Retry("download the snapshot",
			"The connection to the container dropped partway through the download.", err)
	}
	return store.GetResult{Size: opts.Offset + n, Digest: hex.EncodeToString(h.Sum(nil))}, nil
}

// List returns every blob under a prefix.
func (s *Store) List(ctx context.Context, prefix string) ([]store.ObjectInfo, error) {
	full := s.name_(prefix)
	pager := s.client.NewListBlobsFlatPager(s.container, &azblob.ListBlobsFlatOptions{
		Prefix: strPtr(full),
	})

	var out []store.ObjectInfo
	for pager.More() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, classify(err, s.container)
		}
		for _, b := range page.Segment.BlobItems {
			if b == nil || b.Name == nil {
				continue
			}
			key := s.unname(*b.Name)
			if strings.HasSuffix(key, ".portcloak-probe") {
				continue
			}
			info := store.ObjectInfo{Key: key}
			if b.Properties != nil {
				if b.Properties.ContentLength != nil {
					info.Size = *b.Properties.ContentLength
				}
				if b.Properties.LastModified != nil {
					info.ModTime = *b.Properties.LastModified
				}
				if b.Properties.ETag != nil {
					info.ETag = strings.Trim(string(*b.Properties.ETag), `"`)
				}
			}
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Delete removes a blob. Deleting one that is not there has already reached the
// desired end state.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.blob(key).Delete(ctx, nil)
	if err != nil && !bloberror.HasCode(err, bloberror.BlobNotFound) {
		return classify(err, s.container)
	}
	return nil
}

// encodeBlockID produces the fixed-width, base64 block id Azure requires. All
// ids for one blob must be the same length, which is why this is a fixed
// encoding rather than a formatted number.
func encodeBlockID(n int) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(n))
	return base64.StdEncoding.EncodeToString(b[:])
}

func decodeBlockID(id string) int {
	b, err := base64.StdEncoding.DecodeString(id)
	if err != nil || len(b) != 8 {
		return 0
	}
	return int(binary.BigEndian.Uint64(b))
}

func streamOf(b []byte) io.ReadSeekCloser {
	return nopSeekCloser{strings.NewReader(string(b))}
}

type nopSeekCloser struct{ *strings.Reader }

func (nopSeekCloser) Close() error { return nil }

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// containerClientUnused keeps the container import meaningful while container
// creation is the only operation that needs it.
var _ = container.CreateOptions{}
