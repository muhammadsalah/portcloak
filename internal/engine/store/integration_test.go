//go:build integration

// The network backends run the same contract table as disk. A divergence is a
// bug in the newest implementation, not a reason to fork the table.
//
// These tests are behind a build tag rather than a runtime probe on purpose: a
// missing MinIO produces "not run", never a silent pass. A green board that
// quietly skipped every backend test is worse than a red one.
package store_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/store/azurestore"
	"portcloak/internal/engine/store/s3store"
	"portcloak/internal/engine/store/sftpstore"
	"portcloak/internal/engine/store/storetest"
)

// env reads a service-container setting, failing the test rather than skipping:
// under the integration tag, a missing setting is a broken CI configuration.
func env(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s is not set. The integration suite expects the service containers from CI; see spec/rollout/00 §0.7.", key)
	}
	return v
}

func uniquePrefix(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("portcloak-test/%s-%d", t.Name(), time.Now().UnixNano())
}

func TestS3_Contract(t *testing.T) {
	endpoint := env(t, "PORTCLOAK_TEST_S3_ENDPOINT")
	bucket := env(t, "PORTCLOAK_TEST_S3_BUCKET")
	credential := env(t, "PORTCLOAK_TEST_S3_CREDENTIAL")

	storetest.RunContract(t, func(t *testing.T) store.BlobStore {
		creds := config.NewMemoryCredentials()
		handle := config.Handle("s3", "test")
		if err := creds.Set(handle, credential); err != nil {
			t.Fatal(err)
		}
		s, err := s3store.New(context.Background(), config.Storage{
			Name: "test", Kind: config.StoreS3,
			Endpoint: endpoint, Bucket: bucket, Region: "us-east-1",
			Prefix: uniquePrefix(t), PathStyle: true,
			CredentialRef: handle,
		}, creds)
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestS3_ResumableContract(t *testing.T) {
	endpoint := env(t, "PORTCLOAK_TEST_S3_ENDPOINT")
	bucket := env(t, "PORTCLOAK_TEST_S3_BUCKET")
	credential := env(t, "PORTCLOAK_TEST_S3_CREDENTIAL")

	storetest.RunResumableContract(t, func(t *testing.T) store.ResumableStore {
		creds := config.NewMemoryCredentials()
		handle := config.Handle("s3", "test")
		if err := creds.Set(handle, credential); err != nil {
			t.Fatal(err)
		}
		s, err := s3store.New(context.Background(), config.Storage{
			Name: "test", Kind: config.StoreS3,
			Endpoint: endpoint, Bucket: bucket, Region: "us-east-1",
			Prefix: uniquePrefix(t), PathStyle: true, PartSizeMB: 5,
			CredentialRef: handle,
		}, creds)
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestAzure_Contract(t *testing.T) {
	connection := env(t, "PORTCLOAK_TEST_AZURE_CONNECTION")
	containerName := env(t, "PORTCLOAK_TEST_AZURE_CONTAINER")

	storetest.RunContract(t, func(t *testing.T) store.BlobStore {
		creds := config.NewMemoryCredentials()
		handle := config.Handle("azure", "test")
		if err := creds.Set(handle, connection); err != nil {
			t.Fatal(err)
		}
		s, err := azurestore.New(config.Storage{
			Name: "test", Kind: config.StoreAzure,
			Container: containerName, Prefix: uniquePrefix(t),
			CredentialRef: handle,
		}, creds)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.EnsureContainer(context.Background()); err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestAzure_ResumableContract(t *testing.T) {
	connection := env(t, "PORTCLOAK_TEST_AZURE_CONNECTION")
	containerName := env(t, "PORTCLOAK_TEST_AZURE_CONTAINER")

	storetest.RunResumableContract(t, func(t *testing.T) store.ResumableStore {
		creds := config.NewMemoryCredentials()
		handle := config.Handle("azure", "test")
		if err := creds.Set(handle, connection); err != nil {
			t.Fatal(err)
		}
		s, err := azurestore.New(config.Storage{
			Name: "test", Kind: config.StoreAzure,
			Container: containerName, Prefix: uniquePrefix(t), BlockSizeMB: 1,
			CredentialRef: handle,
		}, creds)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.EnsureContainer(context.Background()); err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestSFTP_Contract(t *testing.T) {
	host := env(t, "PORTCLOAK_TEST_SSH_HOST")
	user := env(t, "PORTCLOAK_TEST_SSH_USER")
	credential := env(t, "PORTCLOAK_TEST_SSH_CREDENTIAL")
	folder := env(t, "PORTCLOAK_TEST_SSH_FOLDER")

	storetest.RunContract(t, func(t *testing.T) store.BlobStore {
		creds := config.NewMemoryCredentials()
		handle := config.Handle("ssh", "test")
		if err := creds.Set(handle, credential); err != nil {
			t.Fatal(err)
		}
		s, err := sftpstore.New(config.Storage{
			Name: "test", Kind: config.StoreSSH,
			Host: host, Port: 22, User: user, Auth: config.SSHPassword,
			Folder:        fmt.Sprintf("%s/%s", folder, uniquePrefix(t)),
			CredentialRef: handle,
		}, creds)
		if err != nil {
			t.Fatal(err)
		}
		// A first connection to a throwaway test host is accepted deliberately;
		// in the application this is always an operator's decision.
		s.AcceptHostKey()
		if err := s.EnsureRoot(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}
