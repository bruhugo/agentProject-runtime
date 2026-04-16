package blobstorage

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bruhugo/PicoClawProjectRuntime/include/config"
	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
)

type S3Bucket struct {
	s3Client  *s3.Client
	changeMap map[string]time.Time
}

func NewS3Bucket() *S3Bucket {
	return &S3Bucket{}
}

func (bucket *S3Bucket) Start(ctx context.Context) error {
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(config.AppConfig.S3Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				config.AppConfig.S3AccessKeyID,
				config.AppConfig.S3SecretAccessKey,
				"",
			),
		))

	if err != nil {
		return fmt.Errorf("error starting s3 client: %w", err)
	}

	bucket.s3Client = s3.NewFromConfig(awsConfig)
	bucket.changeMap = make(map[string]time.Time, 0)
	return nil
}

func (bucket *S3Bucket) GetFile(ctx context.Context, filepath, remotepath string) error {
	getObjectInput := &s3.GetObjectInput{
		Bucket: &config.AppConfig.S3Bucket,
		Key:    &remotepath,
	}

	out, err := bucket.s3Client.GetObject(ctx, getObjectInput)
	if err != nil {
		return fmt.Errorf("error retrieving object %s: %w", remotepath, err)
	}

	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("error to open/create file %s: %w", filepath, err)
	}

	_, err = io.Copy(file, out.Body)
	if err != nil {
		return fmt.Errorf("error copying remote file content: %w", err)
	}

	return nil
}

func (bucket *S3Bucket) GetDir(ctx context.Context, dirpath, remotepath string) error {
	_, err := os.Stat(dirpath)
	if os.IsNotExist(err) {
		if err = os.Mkdir(dirpath, fs.ModeDir); err != nil {
			return fmt.Errorf("error creating directory %s: %w", dirpath, err)
		}
	} else {
		return err
	}

	return filepath.WalkDir(dirpath, func(path string, d fs.DirEntry, err error) error {
		itemRemotepath := filepath.Join(remotepath, d.Name())
		if err != nil {
			return err
		}

		if d.IsDir() {
			return bucket.GetDir(ctx, path, itemRemotepath)
		}

		return bucket.GetFile(ctx, path, itemRemotepath)
	})

}

func (bucket *S3Bucket) UploadFile(ctx context.Context, filepath, remotepath string) error {
	file, err := os.OpenFile(filepath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("error opening file %s: %w", filepath, err)
	}
	_, err = bucket.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Key:    &remotepath,
		Bucket: &config.AppConfig.S3Bucket,
		Body:   file,
	})

	if err != nil {
		return fmt.Errorf("error uploading file %s to S3: %w", filepath, err)
	}
	return nil
}

func (bucket *S3Bucket) UploadDir(ctx context.Context, dirpath, remotepath string) error {
	slog.Debug("uploading directory", "path", dirpath)

	stat, err := os.Stat(dirpath)
	if err != nil {
		return fmt.Errorf("error opening directory")
	}
	lastChanged, ok := bucket.changeMap[dirpath]
	if ok && stat.ModTime().Equal(lastChanged) {
		slog.Debug("directory unchanged", "path", dirpath)
		return nil
	}
	bucket.changeMap[dirpath] = stat.ModTime()

	return filepath.WalkDir(dirpath, func(path string, d fs.DirEntry, err error) error {
		itemRemotepath := filepath.Join(remotepath, d.Name())

		if err != nil {
			return fmt.Errorf("failed to traverse directory")
		}
		if d.IsDir() {
			if err = bucket.UploadDir(ctx, path, itemRemotepath); err != nil {
				return err
			}
			return nil
		}

		if err = bucket.UploadFile(ctx, path, itemRemotepath); err != nil {
			return err
		}
		return nil
	})
}

func (bucket *S3Bucket) deleteFile(ctx context.Context, key string) error {
	deleteInput := &s3.DeleteObjectInput{
		Bucket: &config.AppConfig.S3Bucket,
		Key:    aws.String(key),
	}

	_, err := bucket.s3Client.DeleteObject(ctx, deleteInput)
	if err != nil {
		return fmt.Errorf("error deleting file %s: %w", key, err)
	}

	return nil
}

func (bucket *S3Bucket) SyncWorkspace(ctx context.Context, agent *types.Agent) error {
	workspacePath := agent.GetHostWorkspacePath()
	remotePath := agent.GetHostWorkspacePath()

	// first delete the ones that do not exist localy
	listInput := &s3.ListObjectsInput{
		MaxKeys: aws.Int32(2000),
		Prefix:  aws.String(remotePath),
		Bucket:  &config.AppConfig.S3Bucket,
	}

	output, err := bucket.s3Client.ListObjects(ctx, listInput)
	if err != nil {
		return nil
	}

	for _, object := range output.Contents {
		objectPathInWorkspace := filepath.Base(*object.Key)
		_, err := os.Stat(filepath.Join(workspacePath, objectPathInWorkspace))
		if err == nil {
			continue
		}

		// file does not exist and needs to be deleted remotely
		if os.IsNotExist(err) {
			if err = bucket.deleteFile(ctx, *object.Key); err != nil {
				return err
			}
		}

		return err
	}

	err = bucket.UploadDir(ctx, workspacePath, remotePath)
	return err
}

func (bucket *S3Bucket) LoadWorkspace(ctx context.Context, agent *types.Agent) error {
	workspacePath := agent.GetHostWorkspacePath()
	remotePath := agent.GetHostWorkspacePath()

	return bucket.GetDir(ctx, workspacePath, remotePath)
}
