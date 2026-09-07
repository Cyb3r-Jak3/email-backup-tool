package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages/transform"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// validatePublicKey reports whether key material names a scheme this tool can
// encrypt to, and what that scheme is. It exists so a config with an unusable
// key fails at startup rather than after the first message is downloaded.
func validatePublicKey(keyMaterial []byte, source string) (transform.Scheme, error) {
	encryptor, err := transform.NewEncryptor(keyMaterial, source)
	if err != nil {
		return "", err
	}
	logger.Info("Encryption key is valid",
		zap.String("source", source),
		zap.String("scheme", string(encryptor.Scheme())),
		zap.String("recipients", encryptor.Recipients()),
	)
	return encryptor.Scheme(), nil
}

// ConfigVersion identifies the schema of a config file. The `version` key is
// the only field guaranteed to exist in every schema, and it selects which
// versioned struct the rest of the document is decoded into.
type ConfigVersion int

const (
	// ConfigVersion1 is the initial schema, see ConfigV1.
	ConfigVersion1 ConfigVersion = 1
)

// LatestConfigVersion is the version written for new config files.
const LatestConfigVersion = ConfigVersion1

// VersionedConfig is implemented by every config schema. Adding a version 2
// means adding a ConfigV2 that implements this interface and registering it in
// newVersionedConfig; nothing else in this file has to change.
type VersionedConfig interface {
	// ConfigVersion reports the schema version the implementation represents.
	ConfigVersion() ConfigVersion
	// Validate reports whether the decoded config is usable.
	Validate() error
	// Lookup resolves one of the logical keys listed in config_keys.go to its
	// string form, reporting false when the config does not set it. This is
	// the seam the CLI flags read through: a later schema version can store a
	// value under a different field name and still answer the same key.
	Lookup(key string) (string, bool)
}

// newVersionedConfig returns an empty config of the requested schema version.
func newVersionedConfig(version ConfigVersion) (VersionedConfig, error) {
	switch version {
	case ConfigVersion1:
		return &ConfigV1{}, nil
	default:
		return nil, fmt.Errorf("unsupported config version %d (supported: 1)", version)
	}
}

// ConfigFile is the envelope around a config document. It reads the `version`
// key first and then decodes the remainder of the document into the matching
// schema, so files of different versions round trip through the same type.
//
// Callers that need the concrete schema type assert on Config:
//
//	if v1, ok := file.Config.(*ConfigV1); ok { ... }
type ConfigFile struct {
	Version ConfigVersion
	Config  VersionedConfig
}

// NewConfigFile wraps a versioned config for marshalling.
func NewConfigFile(cfg VersionedConfig) *ConfigFile {
	return &ConfigFile{Version: cfg.ConfigVersion(), Config: cfg}
}

// LoadConfigFile reads, decodes and validates a YAML config file from disk.
func LoadConfigFile(path string) (*ConfigFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is user supplied by design
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	var file ConfigFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if err := file.Config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &file, nil
}

// WriteConfigFile marshals the config file and writes it to path.
func WriteConfigFile(path string, file *ConfigFile) error {
	data, err := yaml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}

// UnmarshalYAML decodes the version key, then decodes the whole document into
// the struct registered for that version.
func (f *ConfigFile) UnmarshalYAML(node *yaml.Node) error {
	var envelope struct {
		Version *ConfigVersion `yaml:"version"`
	}
	if err := node.Decode(&envelope); err != nil {
		return err
	}
	if envelope.Version == nil {
		return fmt.Errorf("config is missing the required `version` key")
	}
	cfg, err := newVersionedConfig(*envelope.Version)
	if err != nil {
		return err
	}
	if err := node.Decode(cfg); err != nil {
		return err
	}
	f.Version = *envelope.Version
	f.Config = cfg
	return nil
}

// MarshalYAML emits `version` followed by the fields of the versioned config,
// flattened into a single mapping so the output matches the input layout.
func (f ConfigFile) MarshalYAML() (any, error) {
	if f.Config == nil {
		return nil, fmt.Errorf("config file has no config to marshal")
	}
	var body yaml.Node
	if err := body.Encode(f.Config); err != nil {
		return nil, err
	}
	if body.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config version %d did not encode to a mapping", f.Config.ConfigVersion())
	}
	out := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	out.Content = append(out.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "version"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(int(f.Config.ConfigVersion()))},
	)
	// The versioned structs do not carry a version field themselves, but drop
	// one if a future schema ever adds it so the key is not emitted twice.
	for i := 0; i+1 < len(body.Content); i += 2 {
		if body.Content[i].Value == "version" {
			continue
		}
		out.Content = append(out.Content, body.Content[i], body.Content[i+1])
	}
	return out, nil
}

//go:generate go run ../internal/gen-schema

// ConfigV1 is the version 1 schema.
//
// The `desc`, `enum`, `pattern`, `min`, `max` and `required` struct tags below
// are not read by the yaml/json decoders; they are the source data for the
// generated JSON Schema (see schema.go and config.schema.json). Keep them
// next to the field they describe so a field change and its schema stay in
// the same diff.
type ConfigV1 struct {
	Login   LoginV1    `yaml:"login" json:"login" desc:"Stage 1: where the mail is downloaded from."`
	Encrypt *EncryptV1 `yaml:"encrypt,omitempty" json:"encrypt,omitempty" desc:"Stage 2: the public key mail is encrypted to. Omit to store mail unencrypted."`
	Save    SaveV1     `yaml:"save" json:"save" desc:"Stage 3: where mail is stored. Set as many destinations as you want."`
}

// ConfigVersion implements VersionedConfig.
func (*ConfigV1) ConfigVersion() ConfigVersion { return ConfigVersion1 }

// LoginV1 holds the credentials used to reach the mail server.
type LoginV1 struct {
	IMAP *IMAPLoginV1 `yaml:"imap,omitempty" json:"imap,omitempty" required:"true" desc:"The IMAP server mail is fetched from."`
}

// IMAPLoginV1 is an IMAP server and its credentials.
type IMAPLoginV1 struct {
	Host     string `yaml:"host" json:"host" desc:"IMAP server hostname."`
	Port     int    `yaml:"port" json:"port" min:"0" max:"65535" default:"993" desc:"IMAP server port."`
	Username string `yaml:"username" json:"username" desc:"IMAP account username."`
	Password string `yaml:"password" json:"password" desc:"IMAP account password."`
	// TLS is STARTTLS, SSL or NONE.
	TLS string `yaml:"tls,omitempty" json:"tls,omitempty" enum:"STARTTLS,SSL,NONE" desc:"How to establish TLS with the server."`
	// CACertFile is an optional path to a CA certificate used to verify the
	// server, in place of the system trust store.
	CACertFile string `yaml:"ca_cert_file,omitempty" json:"ca_cert_file,omitempty" desc:"Path to a CA certificate to verify the server, in place of the system trust store."`
	// IgnoreServerCert disables verification of the server certificate.
	IgnoreServerCert bool   `yaml:"ignore_server_cert,omitempty" json:"ignore_server_cert,omitempty" desc:"Disable verification of the server certificate."`
	Mailbox          string `yaml:"mailbox,omitempty" json:"mailbox,omitempty" desc:"Mailbox to back up. Defaults to every mailbox."`
}

// EncryptV1 selects the public key used to encrypt saved mail. At most one of
// PublicKeyFile, PublicKeyString and PublicKeyFingerprint may be set.
type EncryptV1 struct {
	PublicKeyFile   string `yaml:"public_key_file,omitempty" json:"public_key_file,omitempty" desc:"Path to a local age or OpenPGP public key file. Set at most one of public_key_file, public_key_string and public_key_fingerprint."`
	PublicKeyString string `yaml:"public_key_string,omitempty" json:"public_key_string,omitempty" desc:"An age or armoured OpenPGP public key, given inline."`
	// PublicKeyFingerprint fetches an OpenPGP public key by its full
	// fingerprint from PublicKeyServer instead of reading one locally.
	PublicKeyFingerprint string `yaml:"public_key_fingerprint,omitempty" json:"public_key_fingerprint,omitempty" pattern:"^[0-9A-Fa-f ]{40,}$" desc:"Full 40-hex-digit v4 OpenPGP fingerprint to fetch from public_key_server. Spaces are ignored."`
	// PublicKeyServer is the keyserver PublicKeyFingerprint is looked up on.
	// Defaults to transform.DefaultKeyServer (keys.openpgp.org).
	PublicKeyServer string `yaml:"public_key_server,omitempty" json:"public_key_server,omitempty" default:"keys.openpgp.org" desc:"Keyserver host public_key_fingerprint is looked up on. Requires public_key_fingerprint."`
}

// SaveV1 lists the destinations mail is written to. At least one must be set.
type SaveV1 struct {
	Local *SaveLocalV1 `yaml:"local,omitempty" json:"local,omitempty" desc:"Save mail to a directory on disk."`
	S3    *SaveS3V1    `yaml:"s3,omitempty" json:"s3,omitempty" desc:"Save mail to an S3-compatible bucket."`
	Tar   *SaveTarV1   `yaml:"tar,omitempty" json:"tar,omitempty" desc:"Save mail into a tar archive."`
}

// SaveLocalV1 writes mail to a directory on disk.
type SaveLocalV1 struct {
	Directory string `yaml:"directory" json:"directory" desc:"Directory mail is written to."`
}

// SaveS3V1 writes mail to an S3 compatible bucket. Empty credentials fall back
// to the ambient AWS configuration.
type SaveS3V1 struct {
	Bucket          string `yaml:"bucket" json:"bucket" desc:"Destination bucket name."`
	Region          string `yaml:"region,omitempty" json:"region,omitempty" desc:"AWS region the bucket is in."`
	AccessKeyID     string `yaml:"access_key_id,omitempty" json:"access_key_id,omitempty" desc:"Access key ID. Omit to use the ambient AWS configuration."`
	SecretAccessKey string `yaml:"secret_access_key,omitempty" json:"secret_access_key,omitempty" desc:"Secret access key. Omit to use the ambient AWS configuration."`
	Endpoint        string `yaml:"endpoint,omitempty" json:"endpoint,omitempty" desc:"URL of an S3-compatible service. Omit for AWS."`
	// Prefix is placed in front of every uploaded object key.
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty" desc:"Key prefix applied to every uploaded object."`
}

// SaveTarV1 writes mail into a tar archive.
type SaveTarV1 struct {
	Path string `yaml:"path" json:"path" desc:"Path of the tar archive to write."`
}

// Validate implements VersionedConfig.
func (c *ConfigV1) Validate() error {
	if c.Login.IMAP == nil {
		return fmt.Errorf("login.imap is required")
	}
	imap := c.Login.IMAP
	if imap.Host == "" {
		return fmt.Errorf("login.imap.host is required")
	}
	if imap.Port < 0 || imap.Port > 65535 {
		return fmt.Errorf("login.imap.port %d is out of range", imap.Port)
	}
	if imap.Username == "" {
		return fmt.Errorf("login.imap.username is required")
	}
	if _, err := NormalizeTLS(imap.TLS); err != nil {
		return fmt.Errorf("login.imap.tls: %w", err)
	}
	if c.Encrypt != nil {
		set := 0
		for _, v := range []string{c.Encrypt.PublicKeyFile, c.Encrypt.PublicKeyString, c.Encrypt.PublicKeyFingerprint} {
			if v != "" {
				set++
			}
		}
		if set > 1 {
			return fmt.Errorf("encrypt: set only one of public_key_file, public_key_string or public_key_fingerprint")
		}
		if c.Encrypt.PublicKeyServer != "" && c.Encrypt.PublicKeyFingerprint == "" {
			return fmt.Errorf("encrypt: public_key_server requires public_key_fingerprint")
		}
	}
	// An absent encrypt block is valid: the backup is then stored unencrypted,
	// which the backup command warns about.
	if c.Encrypt != nil && c.Encrypt.PublicKeyFile != "" {
		keyMaterial, err := os.ReadFile(c.Encrypt.PublicKeyFile) //nolint:gosec // path is user supplied by design
		if err != nil {
			return fmt.Errorf("encrypt.public_key_file %s: %w", c.Encrypt.PublicKeyFile, err)
		}
		if _, err := validatePublicKey(keyMaterial, c.Encrypt.PublicKeyFile); err != nil {
			return fmt.Errorf("encrypt.public_key_file: %w", err)
		}
	}
	if c.Encrypt != nil && c.Encrypt.PublicKeyString != "" {
		if _, err := validatePublicKey([]byte(c.Encrypt.PublicKeyString), "encrypt.public_key_string"); err != nil {
			return err
		}
	}
	if c.Encrypt != nil && c.Encrypt.PublicKeyFingerprint != "" {
		if _, err := transform.NormalizeFingerprint(c.Encrypt.PublicKeyFingerprint); err != nil {
			return fmt.Errorf("encrypt.public_key_fingerprint: %w", err)
		}
		// The key itself is not fetched here: doing so would mean every command
		// that reads the config, including ones that never encrypt anything,
		// depends on network access to a keyserver. buildTransformer fetches
		// and validates it when the backup actually runs.
	}
	if c.Save.Local == nil && c.Save.S3 == nil && c.Save.Tar == nil {
		return fmt.Errorf("save requires at least one of local, s3 or tar")
	}
	if c.Save.Local != nil && c.Save.Local.Directory == "" {
		return fmt.Errorf("save.local.directory is required")
	}
	if c.Save.S3 != nil && c.Save.S3.Bucket == "" {
		return fmt.Errorf("save.s3.bucket is required")
	}
	if c.Save.Tar != nil && c.Save.Tar.Path == "" {
		return fmt.Errorf("save.tar.path is required")
	}
	return nil
}
