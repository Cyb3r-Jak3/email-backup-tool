package store

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/Cyb3r-Jak3/email-backup-tool/stages"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.uber.org/zap"
)

// S3Options describes the bucket a backup is uploaded to. It covers both AWS
// and S3-compatible services such as MinIO, Ceph or Backblaze B2.
type S3Options struct {
	// Bucket is the destination bucket. Required.
	Bucket string
	// Prefix is an optional key prefix placed in front of every artifact path.
	Prefix string
	// Region is the bucket's region. Compatible services usually accept any
	// value; "us-east-1" is assumed when this is empty.
	Region string
	// AccessKeyID and SecretAccessKey are static credentials. When both are
	// empty the ambient AWS configuration is used (environment, shared config,
	// instance role).
	AccessKeyID     string
	SecretAccessKey string
	// Endpoint points at an S3-compatible service instead of AWS.
	Endpoint string
	// UsePathStyle addresses buckets as endpoint/bucket rather than as a
	// subdomain. It is forced on when Endpoint is set, which is what most
	// compatible services need.
	UsePathStyle bool
	// Profile selects a named profile from the shared config file. It is ignored when
	// AccessKeyID and SecretAccessKey are set.
	Profile string
	// Logger receives progress. Required.
	Logger *zap.Logger
}

// defaultS3Region is used when no region is configured, since the SDK requires
// one even for services that ignore it.
const defaultS3Region = "us-east-1"

// S3 is stage 3 uploading artifacts to an S3-compatible bucket.
type S3 struct {
	client *s3.Client
	bucket string
	prefix string
	logger *zap.Logger
}

// compile-time check that S3 satisfies stage 3.
var _ stages.Sink = (*S3)(nil)

// NewS3 builds the bucket sink and resolves credentials.
func NewS3(ctx context.Context, opts S3Options) (*S3, error) {
	if opts.Bucket == "" {
		return nil, fmt.Errorf("save.s3.bucket is required")
	}
	region := opts.Region
	if region == "" {
		region = defaultS3Region
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if opts.AccessKeyID != "" || opts.SecretAccessKey != "" {
		if opts.AccessKeyID == "" || opts.SecretAccessKey == "" {
			return nil, fmt.Errorf("save.s3: set both access_key_id and secret_access_key, or neither")
		}
		if opts.Profile != "" {
			return nil, fmt.Errorf("save.s3: profile and static credentials are mutually exclusive")
		}
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opts.AccessKeyID, opts.SecretAccessKey, ""),
		))
	} else if opts.Profile != "" {
		loadOptions = append(loadOptions, awsconfig.WithSharedConfigProfile(opts.Profile))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS configuration: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if opts.Endpoint != "" {
			o.BaseEndpoint = aws.String(opts.Endpoint)
			// Compatible services rarely serve virtual-host style buckets.
			o.UsePathStyle = true
		} else {
			o.UsePathStyle = opts.UsePathStyle
		}
	})

	opts.Logger.Info("Storing backup in an S3 bucket",
		zap.String("bucket", opts.Bucket),
		zap.String("region", region),
		zap.String("endpoint", opts.Endpoint),
		zap.String("prefix", opts.Prefix),
	)
	return &S3{
		client: client,
		bucket: opts.Bucket,
		prefix: strings.Trim(opts.Prefix, "/"),
		logger: opts.Logger,
	}, nil
}

// Name implements stages.Sink.
func (s *S3) Name() string { return "s3:" + s.bucket }

// Store implements stages.Sink by uploading the artifact as an object.
func (s *S3) Store(ctx context.Context, artifact *stages.Artifact) error {
	key := artifact.Path
	if s.prefix != "" {
		key = s.prefix + "/" + key
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(artifact.Data),
		ContentLength: aws.Int64(int64(len(artifact.Data))),
		ContentType:   aws.String(objectContentType(artifact.Path)),
	})
	if err != nil {
		return fmt.Errorf("uploading s3://%s/%s: %w", s.bucket, key, err)
	}
	return nil
}

// Close implements stages.Sink. Objects are uploaded as they arrive, so there
// is nothing to finalize.
func (s *S3) Close() error { return nil }

// objectContentType labels uploaded objects so a bucket browser shows something
// sensible. Encrypted artifacts are opaque bytes whatever they started as.
func objectContentType(path string) string {
	switch {
	case strings.HasSuffix(path, ".enc"):
		return "application/octet-stream"
	case strings.HasSuffix(path, ".eml"):
		return "message/rfc822"
	default:
		return "application/octet-stream"
	}
}
