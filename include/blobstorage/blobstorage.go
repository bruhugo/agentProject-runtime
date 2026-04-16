package blobstorage

import (
	"context"

	"github.com/bruhugo/PicoClawProjectRuntime/include/types"
)

type BlobStorage interface {
	Start(ctx context.Context) error
	GetFile(ctx context.Context, filepath, remotepath string) error
	GetDir(ctx context.Context, dirpath, remotepath string) error
	UploadFile(ctx context.Context, filepath, remotepath string) error
	UploadDir(ctx context.Context, dirpath, remotepath string) error
	SyncWorkspace(ctx context.Context, agent *types.Agent) error
	LoadWorkspace(ctx context.Context, agent *types.Agent) error
}
