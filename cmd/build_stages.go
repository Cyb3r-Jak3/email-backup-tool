package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"github.com/Cyb3r-Jak3/email-backup-tool/stages/fetch"
	"github.com/Cyb3r-Jak3/email-backup-tool/stages/store"
	"github.com/Cyb3r-Jak3/email-backup-tool/stages/transform"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// This file is the only place where flags and config become stages. Each
// builder returns the interface, never a concrete type, so choosing a different
// implementation for a stage is a change here and nowhere else.

// buildFetcher builds stage 1 from the connection flags.
func buildFetcher(cmd *cli.Command) (stages.Fetcher, error) {
	tlsMode, err := NormalizeTLS(cmd.String("tls"))
	if err != nil {
		return nil, err
	}
	return fetch.NewIMAP(fetch.IMAPOptions{
		Server:     cmd.String("server"),
		Username:   cmd.String("username"),
		Password:   cmd.String("password"),
		TLS:        tlsMode,
		CACertFile: cmd.String("ca-cert"),
		Insecure:   cmd.Bool("insecure"),
		Logger:     logger,
	})
}

// buildTransformer builds stage 2. With no public key configured the backup is
// stored in the clear, which is legitimate but worth saying out loud, so the
// pass-through case reports itself on stderr as well as in the log.
//
// The key itself selects the encryption scheme, so there is no separate setting
// that could contradict the key material.
func buildTransformer(ctx context.Context, cmd *cli.Command) (stages.Transformer, error) {
	keyFile := cmd.String("public-key-file")
	keyString := cmd.String("public-key")
	fingerprint := cmd.String("public-key-fingerprint")
	set := 0
	for _, v := range []string{keyFile, keyString, fingerprint} {
		if v != "" {
			set++
		}
	}
	if set > 1 {
		return nil, fmt.Errorf("set only one of --public-key-file, --public-key or --public-key-fingerprint")
	}

	var (
		source      string
		keyMaterial []byte
	)
	switch {
	case keyFile != "":
		data, err := os.ReadFile(keyFile) //nolint:gosec // path is user supplied by design
		if err != nil {
			return nil, fmt.Errorf("reading public key %s: %w", keyFile, err)
		}
		source, keyMaterial = keyFile, data
	case keyString != "":
		source, keyMaterial = "inline public key", []byte(keyString)
	case fingerprint != "":
		server := cmd.String("public-key-server")
		data, err := transform.FetchPublicKey(ctx, fingerprint, server)
		if err != nil {
			return nil, fmt.Errorf("fetching public key %s from %s: %w", fingerprint, server, err)
		}
		source, keyMaterial = fmt.Sprintf("keyserver %s (fingerprint %s)", server, fingerprint), data
	default:
		logger.Warn("No encryption key provided: the backup will be stored unencrypted. " +
			"Set encrypt.public_key_file in the config, or pass --public-key-file, to encrypt it.")
		fmt.Fprintln(os.Stderr,
			"WARNING: no encryption key provided, the backup will be stored unencrypted.\n"+
				"         Set encrypt.public_key_file in the config or pass --public-key-file to encrypt it,\n"+
				"         or run `email-backup-tool generate-key` to create a key pair.")
		return transform.PassThrough{}, nil
	}

	encryptor, err := transform.NewEncryptor(keyMaterial, source)
	if err != nil {
		return nil, err
	}
	logger.Info("Encrypting backup",
		zap.String("source", source),
		zap.String("scheme", string(encryptor.Scheme())),
		zap.String("recipients", encryptor.Recipients()),
	)
	return encryptor, nil
}

// buildSink builds stage 3 from every destination that has been configured,
// combining them when there is more than one.
func buildSink(ctx context.Context, cmd *cli.Command) (stages.Sink, error) {
	var sinks []stages.Sink

	if directory := cmd.String("directory"); directory != "" {
		sink, err := store.NewLocal(directory, logger)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, sink)
	}
	if bucket := cmd.String("s3-bucket"); bucket != "" {
		sink, err := store.NewS3(ctx, store.S3Options{
			Bucket:          bucket,
			Prefix:          cmd.String("s3-prefix"),
			Region:          cmd.String("s3-region"),
			AccessKeyID:     cmd.String("s3-access-key-id"),
			SecretAccessKey: cmd.String("s3-secret-access-key"),
			Endpoint:        cmd.String("s3-endpoint"),
			Profile:         cmd.String("s3-profile"),
			Logger:          logger,
		})
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, sink)
	}
	// The tar sink is built last so that a failure in one of the others does
	// not leave a truncated archive behind.
	if path := cmd.String("tar"); path != "" {
		sink, err := store.NewTar(path, logger)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, sink)
	}

	if len(sinks) == 0 {
		return nil, fmt.Errorf("no storage backend configured (set a save block in the config, or pass --directory, --tar or --s3-bucket)")
	}
	return store.NewMulti(sinks...)
}
