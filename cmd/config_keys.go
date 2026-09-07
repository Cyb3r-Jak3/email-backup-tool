package cmd

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Logical config keys. These are the vocabulary shared between the CLI flags
// and every config schema version: a flag names a key, and the loaded config
// answers it through VersionedConfig.Lookup. They are deliberately independent
// of the YAML layout, so a version 2 file may move or rename the underlying
// fields without touching any flag definition.
const (
	KeyIMAPServer   = "imap.server" // host:port
	KeyIMAPHost     = "imap.host"
	KeyIMAPPort     = "imap.port"
	KeyIMAPUsername = "imap.username"
	KeyIMAPPassword = "imap.password"
	KeyIMAPTLS      = "imap.tls"
	KeyIMAPInsecure = "imap.insecure"
	KeyIMAPCACert   = "imap.ca_cert_file"
	KeyIMAPMailbox  = "imap.mailbox"

	KeyEncryptPublicKeyFile        = "encrypt.public_key_file"
	KeyEncryptPublicKeyString      = "encrypt.public_key_string"
	KeyEncryptPublicKeyFingerprint = "encrypt.public_key_fingerprint"
	KeyEncryptPublicKeyServer      = "encrypt.public_key_server"

	KeySaveLocalDirectory = "save.local.directory"
	KeySaveS3Bucket       = "save.s3.bucket"
	KeySaveS3Region       = "save.s3.region"
	KeySaveS3AccessKey    = "save.s3.access_key_id"
	KeySaveS3SecretKey    = "save.s3.secret_access_key" //nolint:gosec // This is a config key, not a secret in the source code.
	KeySaveS3Endpoint     = "save.s3.endpoint"
	KeySaveS3Prefix       = "save.s3.prefix"
	KeySaveTarPath        = "save.tar.path"
)

// TLS modes accepted for the imap.tls key and the --tls flag.
const (
	// TLSStartTLS upgrades a plaintext connection with STARTTLS.
	TLSStartTLS = "STARTTLS"
	// TLSImplicit wraps the connection in TLS from the first byte.
	TLSImplicit = "SSL"
	// TLSNone disables TLS entirely.
	TLSNone = "NONE"
)

// tlsAliases maps the spellings previously accepted by the --tls flag onto the
// canonical names above, so existing invocations and config files keep working.
var tlsAliases = map[string]string{
	"STARTTLS": TLSStartTLS,
	"SMARTTLS": TLSStartTLS,
	"SSL":      TLSImplicit,
	"TLS":      TLSImplicit,
	"IMPLICIT": TLSImplicit,
	"NONE":     TLSNone,
	"":         "",
}

// NormalizeTLS canonicalizes a TLS mode from a flag or a config file. An empty
// value is passed through so an unset value is not an error here.
func NormalizeTLS(value string) (string, error) {
	canonical, ok := tlsAliases[strings.ToUpper(strings.TrimSpace(value))]
	if !ok {
		return "", fmt.Errorf("invalid TLS mode %q, must be one of: %s, %s, %s", value, TLSStartTLS, TLSImplicit, TLSNone)
	}
	return canonical, nil
}

// Lookup implements VersionedConfig for the version 1 schema.
func (c *ConfigV1) Lookup(key string) (string, bool) {
	switch key {
	case KeyIMAPServer:
		if c.Login.IMAP == nil || c.Login.IMAP.Host == "" {
			return "", false
		}
		if c.Login.IMAP.Port == 0 {
			return c.Login.IMAP.Host, true
		}
		return net.JoinHostPort(c.Login.IMAP.Host, strconv.Itoa(c.Login.IMAP.Port)), true
	case KeyIMAPHost:
		return imapString(c, func(i *IMAPLoginV1) string { return i.Host })
	case KeyIMAPPort:
		if c.Login.IMAP == nil || c.Login.IMAP.Port == 0 {
			return "", false
		}
		return strconv.Itoa(c.Login.IMAP.Port), true
	case KeyIMAPUsername:
		return imapString(c, func(i *IMAPLoginV1) string { return i.Username })
	case KeyIMAPPassword:
		return imapString(c, func(i *IMAPLoginV1) string { return i.Password })
	case KeyIMAPTLS:
		// Normalization failures are reported by the flag validator, which
		// sees the raw value; an unusable mode is passed through unchanged.
		raw, ok := imapString(c, func(i *IMAPLoginV1) string { return i.TLS })
		if !ok {
			return "", false
		}
		if canonical, err := NormalizeTLS(raw); err == nil {
			return canonical, true
		}
		return raw, true
	case KeyIMAPInsecure:
		if c.Login.IMAP == nil || !c.Login.IMAP.IgnoreServerCert {
			return "", false
		}
		return "true", true
	case KeyIMAPCACert:
		return imapString(c, func(i *IMAPLoginV1) string { return i.CACertFile })
	case KeyEncryptPublicKeyFile:
		return encryptString(c, func(e *EncryptV1) string { return e.PublicKeyFile })
	case KeyEncryptPublicKeyString:
		return encryptString(c, func(e *EncryptV1) string { return e.PublicKeyString })
	case KeyEncryptPublicKeyFingerprint:
		return encryptString(c, func(e *EncryptV1) string { return e.PublicKeyFingerprint })
	case KeyEncryptPublicKeyServer:
		return encryptString(c, func(e *EncryptV1) string { return e.PublicKeyServer })
	case KeySaveLocalDirectory:
		if c.Save.Local == nil {
			return "", false
		}
		return nonEmpty(c.Save.Local.Directory)
	case KeySaveS3Bucket, KeySaveS3Region, KeySaveS3AccessKey, KeySaveS3SecretKey, KeySaveS3Endpoint, KeySaveS3Prefix:
		if c.Save.S3 == nil {
			return "", false
		}
		switch key {
		case KeySaveS3Bucket:
			return nonEmpty(c.Save.S3.Bucket)
		case KeySaveS3Region:
			return nonEmpty(c.Save.S3.Region)
		case KeySaveS3AccessKey:
			return nonEmpty(c.Save.S3.AccessKeyID)
		case KeySaveS3Endpoint:
			return nonEmpty(c.Save.S3.Endpoint)
		case KeySaveS3Prefix:
			return nonEmpty(c.Save.S3.Prefix)
		default:
			return nonEmpty(c.Save.S3.SecretAccessKey)
		}
	case KeySaveTarPath:
		if c.Save.Tar == nil {
			return "", false
		}
		return nonEmpty(c.Save.Tar.Path)
	case KeyIMAPMailbox:
		return imapString(c, func(i *IMAPLoginV1) string { return i.Mailbox })
	default:
		return "", false
	}
}

// imapString reads one string field of the IMAP login, if it is set.
func imapString(c *ConfigV1, get func(*IMAPLoginV1) string) (string, bool) {
	if c.Login.IMAP == nil {
		return "", false
	}
	return nonEmpty(get(c.Login.IMAP))
}

// encryptString reads one string field of the encrypt block, if it is set.
func encryptString(c *ConfigV1, get func(*EncryptV1) string) (string, bool) {
	if c.Encrypt == nil {
		return "", false
	}
	return nonEmpty(get(c.Encrypt))
}

// nonEmpty reports a value only when it is set, so an empty config field is a
// lookup miss rather than an empty override of a flag default.
func nonEmpty(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	return value, true
}
