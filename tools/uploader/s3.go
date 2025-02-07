package uploader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/replicate/go/logging"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"
)

type s3Uploader struct {
	S3Client *s3.Client

	// Config values for the uploader itself
	MaxPartUploads       int32
	MultipartPartSize    int64
	MaxPartUploadRetries int32
	MaxMPURetries        int32

	initialized   bool
	mpuBufferPool sync.Pool
}

type S3UploaderConfig struct {
	MaxPartUploads       int32
	MultipartPartSize    int64
	MaxPartUploadRetries int32
	MaxMPURetries        int32
}

type uploadFileDetails struct {
	path   string
	size   int64
	bucket string
	key    string
	bar    *mpb.Bar
}

var (
	logger = logging.New("cog.s3uploader")

	ErrSkippedOperation = errors.New("skipped operation")
)

func NewS3Uploader(client *s3.Client, config *S3UploaderConfig) Uploader {
	uploader := &s3Uploader{
		S3Client: client,
	}

	if config != nil {
		uploader.MaxPartUploads = config.MaxPartUploads
		uploader.MultipartPartSize = config.MultipartPartSize
		uploader.MaxMPURetries = config.MaxMPURetries
		uploader.MaxPartUploadRetries = config.MaxPartUploadRetries
	} else {
		uploader.MaxPartUploads = 1024                // 64 GB default limit
		uploader.MultipartPartSize = 64 * 1024 * 1024 // 64MB
		uploader.MaxMPURetries = 3                    // 3 full upload retries
		uploader.MaxPartUploadRetries = 5             // 5 retries per part
	}

	return uploader
}

func (s *s3Uploader) Initialize() {
	if !s.initialized {
		s.mpuBufferPool = sync.Pool{
			New: func() interface{} {
				b := make([]byte, s.MultipartPartSize)
				return &b
			},
		}
		s.initialized = true
	}
}

func (s *s3Uploader) UploadObject(ctx context.Context, objectPath, bucket, key string, p ProgressConfig) error {
	log := logger.With(logging.GetFields(ctx)...).Sugar()
	filename := filepath.Base(objectPath)
	ctx = logging.AddFields(ctx,
		zapcore.Field{Key: "upload_bucket_id", Type: zapcore.StringType, String: bucket},
		zapcore.Field{Key: "file_name", Type: zapcore.StringType, String: filename},
	)
	// Open the file for uploading
	fileInfo, err := os.Stat(objectPath)
	if err != nil {
		return err
	}

	// Start the progress bar
	trimDesc := p.GetPrefix()
	bar := p.progress.New(fileInfo.Size(),
		mpb.BarStyle().Rbound("|"),
		mpb.PrependDecorators(
			decor.Name(trimDesc+" "),
			decor.Counters(decor.SizeB1024(0), "% .2f / % .2f"),
		),
		mpb.AppendDecorators(
			decor.EwmaETA(decor.ET_STYLE_GO, 30),
			decor.Name(" ] "),
			decor.EwmaSpeed(decor.SizeB1024(0), "% .2f", 30),
		),
	)
	defer bar.Abort(false)

	size := fileInfo.Size()
	fInfo := uploadFileDetails{
		path:   objectPath,
		size:   size,
		bucket: bucket,
		key:    key,
		bar:    bar,
	}
	uploadMinSize := s.MultipartPartSize
	if size <= (uploadMinSize * 3) {
		log.Debugw("File is smaller than multipart size, doing single upload", "size", size, "multipart_size", s.MultipartPartSize)
		return s.uploadObjectToS3(ctx, fInfo)
	}
	log.Debugw("multipart upload sync", "size", size)
	return s.uploadMultipartObjectToS3(ctx, fInfo)
}

func (s *s3Uploader) uploadObjectToS3(ctx context.Context, fInfo uploadFileDetails) error {
	// Read the complete object content to the buffer so that we don't get partial data. This is a throwaway buffer but
	// maxes out at 3x the part size since above that we use multipart upload.
	buffer := bytes.NewBuffer(make([]byte, 0, fInfo.size))
	file, err := os.Open(fInfo.path)
	if err != nil {
		return err
	}
	proxyReader := fInfo.bar.ProxyReader(file)
	defer proxyReader.Close()
	_, err = io.Copy(buffer, proxyReader)
	if err != nil {
		return fmt.Errorf("failed to read object content %s: %w", fInfo.path, err)
	}

	_, err = s.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(fInfo.bucket),
		Key:           aws.String(fInfo.key),
		Body:          bytes.NewReader(buffer.Bytes()),
		ContentLength: aws.Int64(fInfo.size),
	})
	if err != nil {
		return fmt.Errorf("failed to upload object to S3 (%s/%s): %w", fInfo.bucket, fInfo.key, err)
	}

	return nil
}

func (s *s3Uploader) uploadMultipartObjectToS3(ctx context.Context, fInfo uploadFileDetails) error {
	log := logger.With(logging.GetFields(ctx)...).Sugar()

	// Initialize if we haven't already
	if !s.initialized {
		s.Initialize()
	}

	// Create a multipart upload
	log.Debugw("creating multipart upload")
	createOutput, err := s.S3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(fInfo.bucket),
		Key:    aws.String(fInfo.key),
	})
	if err != nil {
		log.Errorw("failed to create multipart upload", "bucket", fInfo.bucket, "key", fInfo.key, "error", err)
		return fmt.Errorf("failed to create multipart upload (%s/%s): %w", fInfo.bucket, fInfo.key, err)
	}

	// Upload the object in parts
	var completedParts []types.CompletedPart

	// Ignoring linter warning - if we have enough parts to cause an int32 overflow I think we have
	// bigger problems than an int32 overflow
	expectedParts := int32(fInfo.size / s.MultipartPartSize) // #nosec G115
	if fInfo.size%s.MultipartPartSize > 0 {
		expectedParts++
	}

	completedParts = make([]types.CompletedPart, expectedParts)

	g, egCtx := errgroup.WithContext(ctx)
	g.SetLimit(int(s.MaxPartUploads))

	log.Debugw("uploading parts", "total_parts", expectedParts, "upload_id", *createOutput.UploadId)
	for partNumber := range expectedParts {
		// Except the start and completed part insertion, we expect partNumber to be 1 indexed (as per
		// S3 MPU expectations), so we adjust it here and do subsequent math where needed.
		partNumber++
		if ctx.Err() != nil {
			// context has been canceled, likely a timeout
			return ctx.Err()
		}

		g.Go(func() error {
			log.Debugw("uploading part: starting", "part_number", partNumber, "total_parts", expectedParts)
			start := int64(partNumber-1) * s.MultipartPartSize
			bufPtr := s.mpuBufferPool.Get().(*[]byte)
			buf := *bufPtr

			file, err := os.Open(fInfo.path)
			if err != nil {
				return err
			}
			_, err = file.Seek(start, 0)
			if err != nil {
				return fmt.Errorf("failed to open file pointer %s: %w", fInfo.path, err)
			}
			proxyReader := fInfo.bar.ProxyReader(file)
			defer proxyReader.Close()
			n, err := proxyReader.Read(buf)
			if err != nil {
				isExpectedUnexpectedEOF := errors.Is(err, io.ErrUnexpectedEOF) && partNumber == expectedParts
				isEOF := errors.Is(err, io.EOF)

				if !(isExpectedUnexpectedEOF || isEOF) {
					log.Errorw("upload part failed: failed to read object content", "part_number", partNumber, "total_parts", expectedParts, "error", err, "size", n)
					return fmt.Errorf("failed to read object content (%s/%s): %w", fInfo.bucket, fInfo.key, err)
				}
			}

			log.Debugw("uploading part: source load", "part_number", partNumber, "total_parts", expectedParts, "size", n)

			buf = buf[:n]

			uploadOutput, err := s.doUploadPart(egCtx, &s3.UploadPartInput{
				Bucket:     aws.String(fInfo.bucket),
				Key:        aws.String(fInfo.key),
				PartNumber: aws.Int32(partNumber),
				UploadId:   createOutput.UploadId,
				Body:       bytes.NewReader(buf),
			})
			if err != nil {
				return fmt.Errorf("failed to upload part (%s/%s): %w", fInfo.bucket, fInfo.key, err)
			}

			// PartNumber is 1 indexed in S3 but the slice is zero indexed
			completedParts[partNumber-1] = types.CompletedPart{
				ETag:       uploadOutput.ETag,
				PartNumber: aws.Int32(partNumber),
			}
			// return the buffer to the pool. At this point nothing should be using it
			s.mpuBufferPool.Put(bufPtr)

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		_ = s.abortMPU(ctx, fInfo.bucket, fInfo.key, *createOutput.UploadId)
		return fmt.Errorf("failed to upload parts (%s/%s): %w", fInfo.bucket, fInfo.key, err)
	}

	// Complete the multipart upload
	_, err = s.S3Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(fInfo.bucket),
		Key:      aws.String(fInfo.key),
		UploadId: createOutput.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	log.Debugw("completed multipart upload", "upload_id", *createOutput.UploadId, "completed_parts", len(completedParts))
	if err != nil {
		_ = s.abortMPU(ctx, fInfo.bucket, fInfo.key, *createOutput.UploadId)
		return fmt.Errorf("failed to complete multipart upload (%s/%s): %w", fInfo.bucket, fInfo.key, err)
	}
	return nil
}

func (s *s3Uploader) abortMPU(ctx context.Context, bucket, key, uploadID string) error {
	log := logger.With(logging.GetFields(ctx)...).Sugar()
	log.Warnw("aborting multipart upload", "bucket", bucket, "key", key, "upload_id", uploadID)
	_, err := s.S3Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		log.Errorw("failed to abort multipart upload", "bucket", bucket, "key", key, "upload_id", uploadID, "error", err)
		return fmt.Errorf("failed to abort multipart upload: %w", err)
	}

	return nil
}

// doUploadPart uploads a part to S3 and handles retries for 429 (example) errors. It returns the s3.UploadPartOutput
func (s *s3Uploader) doUploadPart(ctx context.Context, partInput *s3.UploadPartInput) (*s3.UploadPartOutput, error) {
	loggingFields := append(logging.GetFields(ctx),
		zapcore.Field{Key: "part_number", Type: zapcore.Int64Type, Integer: int64(*partInput.PartNumber)},
		zapcore.Field{Key: "size", Type: zapcore.Int64Type, Integer: partInput.Body.(*bytes.Reader).Size()},
	)
	log := logger.With(loggingFields...).Sugar()

	uploadBody := partInput.Body.(*bytes.Reader)

	log.Debugw("uploading part: target send", "size", uploadBody.Size())

	var uploadOutput *s3.UploadPartOutput
	var err error

	for retry := range s.MaxPartUploadRetries + 1 {
		uploadOutput, err = s.S3Client.UploadPart(ctx, partInput)
		if err == nil {
			log.Debugw("uploading part: complete", "etag", aws.ToString(uploadOutput.ETag))
			return uploadOutput, nil
		}

		var re *awshttp.ResponseError

		if s.MaxPartUploadRetries > 0 && errors.As(err, &re) {
			switch re.HTTPStatusCode() {
			case 408, 429, 500, 502, 503, 504:
				log.Debugw("upload part failed with retryableError", "error", err, "retry_count", retry)

				// Reset the buffer to the start so that we can retry the upload
				if _, seekErr := uploadBody.Seek(0, io.SeekStart); seekErr != nil {
					return nil, fmt.Errorf("failed to seek to start of buffer: %w", seekErr)
				}
				continue
			}
		}
		if err != nil {
			log.Errorw("failed to upload part", "bucket", "error", err)
			return nil, err
		}
	}
	return nil, fmt.Errorf("failed to upload part after %d retries: %w", s.MaxPartUploadRetries, err)
}
