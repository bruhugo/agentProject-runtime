package blobstorage

import (
	"context"
	"io"

	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
)

type BlobStorage interface {
	Start(ctx context.Context) error
	GetFile(ctx context.Context, filepath, remotepath string) error
	GetDir(ctx context.Context, dirpath, remotepath string) error
	UploadFile(ctx context.Context, filepath, remotepath string) error
	UploadDir(ctx context.Context, dirpath, remotepath string) error
	UploadBuffer(ctx context.Context, reader io.Reader, remotepath string) error
	SyncWorkspace(ctx context.Context, agent *types.Agent) error
	LoadWorkspace(ctx context.Context, agent *types.Agent) error
}
