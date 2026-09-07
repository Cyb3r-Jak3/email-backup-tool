//go:build e2e
// +build e2e

// Package e2e drives the real CLI (cmd.BuildApp, the same entry point main.go
// uses) against real backing services started with Docker Compose: a
// GreenMail IMAP server standing in for the mail account, and RustFS
// (https://github.com/rustfs/rustfs) standing in for an S3 bucket. Nothing
// here is mocked at the Go level - only the outside world (a real mail
// account, a real AWS bucket) is replaced by a local equivalent speaking the
// same protocol, so the suite exercises the actual IMAP fetch, age
// encryption, S3/local storage and CLI wiring end to end.
//
// Run with:
//
//	go test -tags e2e ./e2e/...
//
// It requires Docker (with the compose plugin) on PATH; the test skips
// itself when that is not the case. TestMain brings docker-compose.yml up
// once for the whole package and tears it down afterwards.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/Cyb3r-Jak3/email-backup-tool/cmd"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/emersion/go-imap/v2/imapclient"
	"go.uber.org/zap/zaptest"
)

const (
	composeFile = "docker-compose.yml"

	imapAddr     = "127.0.0.1:3143"
	smtpAddr     = "127.0.0.1:3025"
	imapUsername = "e2e"
	imapPassword = "e2e-password"
	imapEmail    = "e2e@example.com"

	s3Endpoint  = "http://127.0.0.1:9000"
	s3AccessKey = "rustfsadmin"
	s3SecretKey = "rustfsadmin"
)

// TestMain brings the backing services up once for every test in the
// package and tears them down afterwards, so individual tests only worry
// about the mail and buckets they add.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("docker not found on PATH, skipping e2e suite")
		os.Exit(0)
	}

	up := exec.Command("docker", "compose", "-f", composeFile, "up", "-d")
	up.Stdout, up.Stderr = os.Stdout, os.Stderr
	if err := up.Run(); err != nil {
		fmt.Println("docker compose up failed:", err)
		os.Exit(1)
	}

	code := func() int {
		defer func() {
			down := exec.Command("docker", "compose", "-f", composeFile, "down", "-v")
			down.Stdout, down.Stderr = os.Stdout, os.Stderr
			_ = down.Run()
		}()

		if err := waitForIMAP(60 * time.Second); err != nil {
			fmt.Println("imap server never became ready:", err)
			return 1
		}
		if err := waitForTCP(smtpAddr, 60*time.Second); err != nil {
			fmt.Println("smtp server never became reachable:", err)
			return 1
		}
		if err := waitForTCP("127.0.0.1:9000", 60*time.Second); err != nil {
			fmt.Println("s3 server never became reachable:", err)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

// waitForTCP polls addr until a connection succeeds or timeout elapses.
func waitForTCP(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			return conn.Close()
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s: %w", addr, lastErr)
}

// waitForIMAP polls the IMAP server with a real login rather than just a TCP
// dial: GreenMail accepts connections on its port before the JVM has finished
// booting the protocol handler, which resets a bare TCP probe's connection.
func waitForIMAP(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	consecutiveOK := 0
	for time.Now().Before(deadline) {
		client, err := imapclient.DialInsecure(imapAddr, nil)
		if err == nil {
			err = client.Login(imapUsername, imapPassword).Wait()
			_ = client.Close()
		}
		if err == nil {
			// A freshly booted GreenMail can accept one login and then reset
			// the next connection while it finishes initializing, so several
			// successful logins in a row are required before it is trusted.
			consecutiveOK++
			if consecutiveOK >= 3 {
				return nil
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		consecutiveOK = 0
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for imap login to succeed: %w", lastErr)
}

// deliverMessage hands one message to the test account over a real SMTP
// submission, exactly as a real mail client would; GreenMail delivers it
// straight into the account's INBOX. Seeding is done over SMTP rather than a
// raw IMAP APPEND because the emersion/go-imap v2 client used here is a beta
// and, empirically against GreenMail, a second command issued on a
// connection that has just done an APPEND is unreliable; net/smtp has no
// such issue, and it exercises real delivery into the mailbox besides.
func deliverMessage(t *testing.T, raw []byte) {
	t.Helper()
	if err := smtp.SendMail(smtpAddr, nil, "sender@example.com", []string{imapEmail}, raw); err != nil {
		t.Fatalf("delivering seed message over smtp: %v", err)
	}
	// GreenMail's SMTP->mailbox handoff is asynchronous by a few milliseconds
	// even though SendMail has already returned; give it a moment so the
	// backup that follows reliably sees the message.
	time.Sleep(300 * time.Millisecond)
}

// plainMessage builds a minimal RFC 5322 message with the given subject and
// body, suitable for deliverMessage.
func plainMessage(subject, body string) []byte {
	now := time.Now().UTC()
	return fmt.Appendf(nil, "From: sender@example.com\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"Date: %s\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"\r\n"+
		"%s\r\n",
		imapEmail, subject, now.Format(time.RFC1123Z), body)
}

// messageWithAttachment builds a multipart message carrying one text-body
// part and one attachment part, so --attachments has something to extract.
func messageWithAttachment(subject, body, attachmentName, attachmentBody string) []byte {
	now := time.Now().UTC()
	return fmt.Appendf(nil, "From: sender@example.com\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"Date: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: multipart/mixed; boundary=e2e-boundary\r\n"+
		"\r\n"+
		"--e2e-boundary\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"\r\n"+
		"%s\r\n"+
		"--e2e-boundary\r\n"+
		"Content-Type: application/octet-stream\r\n"+
		"Content-Disposition: attachment; filename=\"%s\"\r\n"+
		"\r\n"+
		"%s\r\n"+
		"--e2e-boundary--\r\n",
		imapEmail, subject, now.Format(time.RFC1123Z), body, attachmentName, attachmentBody)
}

// runCLI drives the real CLI exactly as main.go does: it builds the app fresh
// (BuildApp resets the package-level logger and other globals cmd relies on,
// so calls must not run concurrently) and runs it with the given arguments.
func runCLI(t *testing.T, args ...string) error {
	t.Helper()
	app := cmd.BuildApp(cmd.BuildArgs{
		Version:   "e2e-test",
		Date:      "now",
		Logger:    zaptest.NewLogger(t),
		StartTime: time.Now(),
	})
	ctx := context.WithValue(t.Context(), cmd.VersionContextKey, "e2e-test")
	return app.Run(ctx, append([]string{"imap-backup-tool"}, args...))
}

// connectionArgs are the flags every backup/login invocation needs to reach
// the GreenMail server started by docker-compose.yml.
func connectionArgs() []string {
	return []string{
		"--server", imapAddr,
		"--username", imapUsername,
		"--password", imapPassword,
		"--tls", "NONE",
	}
}

// s3Client returns a client for the RustFS instance, addressed path-style
// the way store.NewS3 addresses any S3-compatible endpoint.
func s3Client(t *testing.T) *s3.Client {
	t.Helper()
	cfg, err := awsconfig.LoadDefaultConfig(t.Context(),
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s3AccessKey, s3SecretKey, "")),
	)
	if err != nil {
		t.Fatalf("loading aws config: %v", err)
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s3Endpoint)
		o.UsePathStyle = true
	})
}

// makeBucket creates a fresh bucket for a test to store into, so tests never
// have to reconcile their own objects against another test's.
func makeBucket(t *testing.T, client *s3.Client, name string) {
	t.Helper()
	_, err := client.CreateBucket(t.Context(), &s3.CreateBucketInput{Bucket: aws.String(name)})
	if err != nil {
		t.Fatalf("creating bucket %s: %v", name, err)
	}
}

// listKeys returns every object key currently in bucket.
func listKeys(t *testing.T, client *s3.Client, bucket string) []string {
	t.Helper()
	out, err := client.ListObjectsV2(t.Context(), &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
	if err != nil {
		t.Fatalf("listing objects in %s: %v", bucket, err)
	}
	keys := make([]string, 0, len(out.Contents))
	for _, obj := range out.Contents {
		keys = append(keys, *obj.Key)
	}
	return keys
}

// downloadAll fetches every object under bucket into dir, named after the
// last path element of its key, and returns the local paths written.
func downloadAll(t *testing.T, client *s3.Client, bucket, dir string) []string {
	t.Helper()
	var paths []string
	for i, key := range listKeys(t, client, bucket) {
		out, err := client.GetObject(t.Context(), &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		if err != nil {
			t.Fatalf("getting object %s: %v", key, err)
		}
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(out.Body); err != nil {
			t.Fatalf("reading object %s: %v", key, err)
		}
		_ = out.Body.Close()
		path := filepath.Join(dir, fmt.Sprintf("%d-%s", i, filepath.Base(key)))
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		paths = append(paths, path)
	}
	return paths
}

// generateAgeKeyPair writes a private/public age key pair to dir and returns
// their paths, mirroring what `imap-backup-tool generate-key` produces.
func generateAgeKeyPair(t *testing.T, dir string) (privatePath, publicPath string) {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generating age identity: %v", err)
	}
	privatePath = filepath.Join(dir, "key.key")
	publicPath = filepath.Join(dir, "key.pub")
	if err := os.WriteFile(privatePath, []byte(identity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("writing private key: %v", err)
	}
	if err := os.WriteFile(publicPath, []byte(identity.Recipient().String()+"\n"), 0o644); err != nil {
		t.Fatalf("writing public key: %v", err)
	}
	return privatePath, publicPath
}

// randomSuffix gives each test its own bucket/subject namespace so tests
// sharing one IMAP mailbox and one RustFS instance cannot see each other's
// data.
func randomSuffix() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Intn(1_000_000))
}

// TestBackupToS3RoundTrip backs up a plain message and one with an
// attachment from the real IMAP server, encrypted with a real age key, into
// a real S3-compatible bucket, then decrypts what was stored with the
// `decrypt` command and checks the plaintext matches what was sent.
func TestBackupToS3RoundTrip(t *testing.T) {
	suffix := randomSuffix()
	// t.Logf("using bucket: %s", bucket)
	bucket := "e2e-roundtrip-" + strings.ToLower(strings.ReplaceAll(suffix, "_", "-"))
	subject := "roundtrip body " + suffix
	body := "hello from the e2e suite " + suffix
	attachSubject := "roundtrip attachment " + suffix
	attachName := "note.txt"
	attachBody := "attachment payload " + suffix

	deliverMessage(t, plainMessage(subject, body))
	deliverMessage(t, messageWithAttachment(attachSubject, "body text", attachName, attachBody))

	dir := t.TempDir()
	privateKey, publicKey := generateAgeKeyPair(t, dir)

	client := s3Client(t)
	makeBucket(t, client, bucket)

	args := append(connectionArgs(),
		"--all", "--attachments",
		"--public-key-file", publicKey,
		"--s3-bucket", bucket,
		"--s3-endpoint", s3Endpoint,
		"--s3-access-key-id", s3AccessKey,
		"--s3-secret-access-key", s3SecretKey,
		"--timestamp-file", filepath.Join(dir, "watermark"),
	)
	if err := runCLI(t, append([]string{"backup"}, args...)...); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	keys := listKeys(t, client, bucket)
	if len(keys) == 0 {
		t.Fatal("no objects were stored in the bucket")
	}
	for _, key := range keys {
		if !strings.HasSuffix(key, ".age") {
			t.Errorf("object %s is not age-encrypted (missing .age suffix)", key)
		}
	}

	downloadDir := filepath.Join(dir, "downloaded")
	if err := os.MkdirAll(downloadDir, 0o750); err != nil {
		t.Fatal(err)
	}
	encryptedFiles := downloadAll(t, client, bucket, downloadDir)

	decryptedDir := filepath.Join(dir, "decrypted")
	if err := os.MkdirAll(decryptedDir, 0o750); err != nil {
		t.Fatal(err)
	}
	decryptArgs := []string{"decrypt", "--private-key-file", privateKey, "--output", decryptedDir}
	decryptArgs = append(decryptArgs, encryptedFiles...)
	if err := runCLI(t, decryptArgs...); err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	combined := readAllFiles(t, decryptedDir)
	for _, want := range []string{subject, body, attachSubject, attachBody} {
		if !strings.Contains(combined, want) {
			t.Errorf("decrypted output does not contain %q", want)
		}
	}
	// The attachment must also have been broken out into its own file, named
	// after the original attachment (downloadAll prefixes an index to avoid
	// collisions between S3 keys that share a basename, so this checks a
	// suffix rather than an exact name), not just embedded in the .eml.
	foundAttachmentFile := false
	entries, err := os.ReadDir(decryptedDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), attachName) {
			foundAttachmentFile = true
			data, err := os.ReadFile(filepath.Join(decryptedDir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), attachBody) {
				t.Errorf("attachment file content = %q, want it to contain %q", data, attachBody)
			}
		}
	}
	if !foundAttachmentFile {
		t.Errorf("no decrypted file ending in %s (the attachment was not stored separately)", attachName)
	}
}

// readAllFiles concatenates every regular file directly under dir, for a
// simple "does the output contain X" check across several output files.
func readAllFiles(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	return out.String()
}

// TestIncrementalBackupOnlyFetchesNewMessages runs a full backup to establish
// a timestamp watermark, adds one more message to the mailbox, then runs
// again without --all: only the new message should be added to the bucket.
func TestIncrementalBackupOnlyFetchesNewMessages(t *testing.T) {
	suffix := randomSuffix()
	bucket := "e2e-incremental-" + strings.ToLower(strings.ReplaceAll(suffix, "_", "-"))
	dir := t.TempDir()
	_, publicKey := generateAgeKeyPair(t, dir)
	watermark := filepath.Join(dir, "watermark")

	client := s3Client(t)
	makeBucket(t, client, bucket)

	deliverMessage(t, plainMessage("incremental baseline "+suffix, "baseline body"))

	storeArgs := func() []string {
		return append(connectionArgs(),
			"--public-key-file", publicKey,
			"--s3-bucket", bucket,
			"--s3-endpoint", s3Endpoint,
			"--s3-access-key-id", s3AccessKey,
			"--s3-secret-access-key", s3SecretKey,
			"--timestamp-file", watermark,
		)
	}

	if err := runCLI(t, append([]string{"backup", "--all"}, storeArgs()...)...); err != nil {
		t.Fatalf("baseline backup failed: %v", err)
	}
	if _, err := os.Stat(watermark); err != nil {
		t.Fatalf("baseline backup did not write a watermark file: %v", err)
	}
	baselineCount := len(listKeys(t, client, bucket))
	if baselineCount == 0 {
		t.Fatal("baseline backup stored nothing")
	}

	// The watermark has second resolution, so this needs to land in a
	// visibly later second than the baseline message for the incremental
	// run to treat it as new.
	time.Sleep(1100 * time.Millisecond)
	deliverMessage(t, plainMessage("incremental new "+suffix, "new body"))

	if err := runCLI(t, append([]string{"backup"}, storeArgs()...)...); err != nil {
		t.Fatalf("incremental backup failed: %v", err)
	}

	afterCount := len(listKeys(t, client, bucket))
	if afterCount != baselineCount+1 {
		t.Fatalf("bucket has %d objects after the incremental run, want exactly %d (baseline %d + the one new message)",
			afterCount, baselineCount+1, baselineCount)
	}
}

// TestBackupFansOutToLocalAndS3 stores one unencrypted backup to both a local
// directory and an S3 bucket in the same run, checking store.Multi actually
// fans a single artifact out to every configured destination.
func TestBackupFansOutToLocalAndS3(t *testing.T) {
	suffix := randomSuffix()
	bucket := "e2e-multi-" + strings.ToLower(strings.ReplaceAll(suffix, "_", "-"))
	subject := "multi sink " + suffix
	body := "stored in two places " + suffix

	deliverMessage(t, plainMessage(subject, body))

	client := s3Client(t)
	makeBucket(t, client, bucket)

	dir := t.TempDir()
	localDir := filepath.Join(dir, "local")
	if err := os.MkdirAll(localDir, 0o750); err != nil {
		t.Fatal(err)
	}

	args := append(connectionArgs(),
		"--all",
		"--directory", localDir,
		"--s3-bucket", bucket,
		"--s3-endpoint", s3Endpoint,
		"--s3-access-key-id", s3AccessKey,
		"--s3-secret-access-key", s3SecretKey,
		"--timestamp-file", filepath.Join(dir, "watermark"),
	)
	if err := runCLI(t, append([]string{"backup"}, args...)...); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	s3Keys := listKeys(t, client, bucket)
	if len(s3Keys) == 0 {
		t.Fatal("nothing was stored in the bucket")
	}

	// Messages are filed into subdirectories under localDir, so walk for it.
	var foundLocally bool
	if err := filepath.WalkDir(localDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), body) {
			foundLocally = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !foundLocally {
		t.Fatal("local directory does not contain the backed up message")
	}

	// Every local file must also exist as an S3 object with matching content,
	// since PassThrough writes the same bytes to both sinks with no suffix.
	err := filepath.WalkDir(localDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		out, err := client.GetObject(t.Context(), &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		if err != nil {
			t.Errorf("local file %s has no matching S3 object %s: %v", path, key, err)
			return nil
		}
		defer out.Body.Close()
		s3Data := new(bytes.Buffer)
		if _, err := s3Data.ReadFrom(out.Body); err != nil {
			return err
		}
		localData, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(localData, s3Data.Bytes()) {
			t.Errorf("local file %s and S3 object %s have different content", path, key)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestLoginCommand checks the `login` command against the real server: it
// must succeed with the right credentials and fail with the wrong ones.
func TestLoginCommand(t *testing.T) {
	if err := runCLI(t, append([]string{"login"}, connectionArgs()...)...); err != nil {
		t.Errorf("login with correct credentials failed: %v", err)
	}

	badArgs := []string{
		"login",
		"--server", imapAddr,
		"--username", imapUsername,
		"--password", "not-the-password",
		"--tls", "NONE",
	}
	if err := runCLI(t, badArgs...); err == nil {
		t.Error("login with the wrong password succeeded, want an error")
	}
}
