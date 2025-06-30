package uploader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
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

type uploadFileDetails struct {
	path   string
	size   int64
	bucket string
	key    string
	bar    *mpb.Bar
}

var (
	ErrSkippedOperation = errors.New("skipped operation")
)

func parseInt32(s string) (int32, error) {
	out, err := strconv.ParseInt(s, 0, 32)
	return int32(out), err
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 0, 64)
}

func NewS3Uploader(client *s3.Client) Uploader {
	uploader := &s3Uploader{
		S3Client: client,
	}

	// 4096 max parts default - 64 GB default limit (16 MB max part size default * 4096 parts)
	uploader.MaxPartUploads = util.GetEnvOrDefault(UPLOADER_MAX_PARTS_UPLOAD_KEY, 1024*4, parseInt32)
	// 16 MB part size default
	uploader.MultipartPartSize = util.GetEnvOrDefault(UPLOADER_MULTIPART_SIZE_KEY, 16*1024*1024, parseInt64)
	// 3 full upload retries default
	uploader.MaxMPURetries = util.GetEnvOrDefault(UPLOADER_MAX_MPU_RETRIES_KEY, 3, parseInt32)
	// 30 retries per part default
	uploader.MaxPartUploads = util.GetEnvOrDefault(UPLOADER_MAX_PART_UPLOAD_RETRIES_KEY, 30, parseInt32)

	return uploader
}

func (s *s3Uploader) Initialize() {
	if !s.initialized {
		s.mpuBufferPool = sync.Pool{
			New: func() any {
				b := make([]byte, s.MultipartPartSize)
				return &b
			},
		}
		s.initialized = true
	}
}

func (s *s3Uploader) UploadObject(ctx context.Context, objectPath, bucket, key string, p ProgressConfig) error {
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
		mpb.BarRemoveOnComplete(),
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
		return s.uploadObjectToS3(ctx, fInfo)
	}
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
	defer file.Close()
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

	// Initialize if we haven't already
	s.Initialize()

	// Create a multipart upload
	createOutput, err := s.S3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(fInfo.bucket),
		Key:    aws.String(fInfo.key),
	})
	if err != nil {
		console.Errorf("failed to create multipart upload. bucket: %s, key: %s, error: %v", fInfo.bucket, fInfo.key, err)
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

	for partNumber := range expectedParts {
		// Except the start and completed part insertion, we expect partNumber to be 1 indexed (as per
		// S3 MPU expectations), so we adjust it here and do subsequent math where needed.
		partNumber++
		if ctx.Err() != nil {
			// context has been canceled, likely a timeout
			return ctx.Err()
		}

		g.Go(func() error {
			start := int64(partNumber-1) * s.MultipartPartSize
			bufPtr := s.mpuBufferPool.Get().(*[]byte)
			buf := *bufPtr

			file, err := os.Open(fInfo.path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = file.Seek(start, 0)
			if err != nil {
				return fmt.Errorf("failed to open file pointer %s: %w", fInfo.path, err)
			}
			startTime := time.Now()
			n, err := file.Read(buf)
			if err != nil {
				isExpectedUnexpectedEOF := errors.Is(err, io.ErrUnexpectedEOF) && partNumber == expectedParts
				isEOF := errors.Is(err, io.EOF)

				if !(isExpectedUnexpectedEOF || isEOF) {
					console.Errorf("upload part failed: failed to read object content. part_number: %d, total_parts: %d, size: %d, error: %v", partNumber, expectedParts, n, err)
					return fmt.Errorf("failed to read object content (%s/%s): %w", fInfo.bucket, fInfo.key, err)
				}
			}

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

			fInfo.bar.EwmaIncrBy(n, time.Now().Sub(startTime))

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
	if err != nil {
		_ = s.abortMPU(ctx, fInfo.bucket, fInfo.key, *createOutput.UploadId)
		return fmt.Errorf("failed to complete multipart upload (%s/%s): %w", fInfo.bucket, fInfo.key, err)
	}
	return nil
}

func (s *s3Uploader) abortMPU(ctx context.Context, bucket, key, uploadID string) error {
	console.Warnf("aborting multipart upload to bucket %s at key %s with upload_id %s", bucket, key, uploadID)
	_, err := s.S3Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		console.Errorf("failed to abort multipart upload: %v", err)
		return fmt.Errorf("failed to abort multipart upload: %w", err)
	}

	return nil
}

// doUploadPart uploads a part to S3 and handles retries for 429 (example) errors. It returns the s3.UploadPartOutput
func (s *s3Uploader) doUploadPart(ctx context.Context, partInput *s3.UploadPartInput) (*s3.UploadPartOutput, error) {
	uploadBody := partInput.Body.(*bytes.Reader)

	var uploadOutput *s3.UploadPartOutput
	var err error

	for retry := range s.MaxPartUploadRetries + 1 {
		uploadOutput, err = s.S3Client.UploadPart(ctx, partInput)
		if err == nil {
			return uploadOutput, nil
		}

		var re *awshttp.ResponseError

		if s.MaxPartUploadRetries > 0 && errors.As(err, &re) {
			switch re.HTTPStatusCode() {
			case 408, 429, 500, 502, 503, 504:
				console.Warnf("upload part failed with retryableError. retry_count: %d, error: %v", retry, err)

				// Reset the buffer to the start so that we can retry the upload
				if _, seekErr := uploadBody.Seek(0, io.SeekStart); seekErr != nil {
					return nil, fmt.Errorf("failed to seek to start of buffer: %w", seekErr)
				}
				continue
			}
		}
		// Error must be non nil here otherwise we would have escaped out already
		console.Errorf("failed to upload part. error: %v", err)
		return nil, err
	}
	return nil, fmt.Errorf("failed to upload part after %d retries: %w", s.MaxPartUploadRetries, err)
}
