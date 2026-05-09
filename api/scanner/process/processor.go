package process

import (
	"context"
	"fmt"

	"github.com/photoview/photoview/api/database"
	"github.com/photoview/photoview/api/graphql/models"
	"github.com/photoview/photoview/api/log"
	"github.com/photoview/photoview/api/scanner/tools/exif"
	ignore "github.com/sabhiram/go-gitignore"
	"gorm.io/gorm"
)

type Processor struct {
	baseCtx context.Context
	db      *gorm.DB
	exif    *exif.Exiftool
	ignore  *ignore.GitIgnore
}

func NewProcessor(ctx context.Context) (*Processor, error) {
	db := database.DB(ctx)
	if db == nil {
		return nil, fmt.Errorf("create processor: can't find db instance")
	}

	exiftool, err := exif.New()
	if err != nil {
		return nil, fmt.Errorf("create processor: %w", err)
	}

	return &Processor{
		baseCtx: log.WithAttrs(ctx, "unit", "processor", "id", 1),
		db:      db,
		exif:    exiftool,
	}, nil
}

func (p *Processor) Close() error {
	return p.exif.Close()
}

func (p *Processor) Process(album *models.Album, file string) {
	ctx := log.WithAttrs(p.baseCtx, "original_path", file)

	task, err := newTask(album, file)
	if err != nil {
		log.Warn(ctx, "create task error", "err", err)
	}

	if err := p.CollectSidecars(ctx, task); err != nil {
		return
	}

	if !task.IsValid() {
		return
	}

	if err := p.LoadMedia(ctx, task); err != nil {
		return
	}

	if err := p.SyncToMedia(ctx, task); err != nil {
		return
	}

	if err := p.Commit(ctx, task); err != nil {
		return
	}
}
