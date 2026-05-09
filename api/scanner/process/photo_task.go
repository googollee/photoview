package process

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/photoview/photoview/api/graphql/models"
	"github.com/photoview/photoview/api/scanner/media_type"
	"github.com/photoview/photoview/api/scanner/tools/image"
	"github.com/photoview/photoview/api/utils"
)

type photoTask struct {
	*task

	HighRes file
}

func newPhotoTask(t *task) *photoTask {
	ret := &photoTask{task: t}
	return ret
}

func (t *photoTask) Execute(ctx context.Context) error {
	for _, media := range t.MediaURLs {
		switch media.Purpose {
		case models.PhotoHighRes:
			file, err := media.CachedPath()
			if err != nil {
				return fmt.Errorf("get highres name for %q error: %w", t.Original.Path, err)
			}

			highres, err := newFile(file)
			if err != nil {
				return fmt.Errorf("read highres %q for %q error: %w", file, t.Original.Path, err)
			}

			t.HighRes = highres
		case models.PhotoThumbnail:
		}
	}

	return nil
}

func (t *photoTask) ensureSidecars(cachePath string) error {
	_, hasHighRes := t.MediaURLs[models.PhotoHighRes]
	_, hasThumbnail := t.MediaURLs[models.PhotoThumbnail]
	if hasHighRes && hasThumbnail {
		return nil
	}

	wand, err := image.New(t.Original.Path)
	if err != nil {
		return fmt.Errorf("read photo %q error: %w", t.Original.Path, err)
	}
	defer wand.Close()

	if highRes := t.HighRes; highRes.Path == "" {
		highRes.Path = generateUniqueMediaNamePrefixed(cachePath, "highres", t.Original.Path, ".jpg")

		if err := wand.EncodeJpeg(70); err != nil {
			return fmt.Errorf("create highres for %q error: %w", t.Original.Path, err)
		}
		if err := wand.Save(highRes.Path); err != nil {
			return fmt.Errorf("write highres for %q error: %w", t.Original.Path, err)
		}

		highRes.Type = media_type.TypeJPEG

		fi, err := os.Stat(highRes.Path)
		if err != nil {
			return fmt.Errorf("get fileinfo for %q error: %w", highRes.Path, err)
		}
		highRes.Info = fi

		t.HighRes = highRes
	}

	if !hasHighRes {
		t.MediaURLs[models.PhotoHighRes] = &models.MediaURL{
			MediaName:   t.Media.Title,
			Width:       int(t.OriginalDimension.Width()),
			Height:      int(t.OriginalDimension.Height()),
			Purpose:     models.PhotoHighRes,
			ContentType: t.HighRes.Type.String(),
			FileSize:    t.HighRes.Info.Size(),
		}
	}

	if thumbnail := t.Thumbnail; thumbnail.Path == "" {
		thumbnail.Path = generateUniqueMediaNamePrefixed(cachePath, "thumbnail", t.Original.Path, ".jpg")

		if err := wand.GenerateThumbnail(t.ThumbnailDimension.Width(), t.ThumbnailDimension.Height()); err != nil {
			return fmt.Errorf("create thumbnail for %q error: %w", t.Original.Path, err)
		}
		if err := wand.EncodeJpeg(70); err != nil {
			return fmt.Errorf("create thumbnail for %q error: %w", t.Original.Path, err)
		}
		if err := wand.Save(thumbnail.Path); err != nil {
			return fmt.Errorf("write thumbnail for %q error: %w", t.Original.Path, err)
		}

		thumbnail.Type = media_type.TypeJPEG

		fi, err := os.Stat(thumbnail.Path)
		if err != nil {
			return fmt.Errorf("get fileinfo for %q error: %w", thumbnail.Path, err)
		}
		thumbnail.Info = fi

		t.HighRes = thumbnail
	}

	if !hasThumbnail {
		t.MediaURLs[models.PhotoThumbnail] = &models.MediaURL{
			MediaName:   t.Media.Title,
			Width:       int(t.ThumbnailDimension.Width()),
			Height:      int(t.ThumbnailDimension.Height()),
			Purpose:     models.PhotoHighRes,
			ContentType: t.Thumbnail.Type.String(),
			FileSize:    t.Thumbnail.Info.Size(),
		}
	}

	return nil
}

func generateUniqueMediaNamePrefixed(cachePath, prefix, mediaPath, extension string) string {
	mediaName := fmt.Sprintf("%s_%s_%s", prefix, path.Base(mediaPath), utils.GenerateToken())
	mediaName = models.SanitizeMediaName(mediaName)
	mediaName = filepath.Join(cachePath, mediaName+extension)
	return mediaName
}
