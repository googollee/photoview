package process

import (
	"context"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Kagami/go-face"
	"github.com/buckket/go-blurhash"
	"github.com/photoview/photoview/api/graphql/models"
	"github.com/photoview/photoview/api/scanner/media_type"
	"github.com/photoview/photoview/api/scanner/tools/exif"
	"github.com/photoview/photoview/api/utils"
	"github.com/pkg/errors"
	ignore "github.com/sabhiram/go-gitignore"
	"gorm.io/gorm"
)

type executableTask interface {
	Execute(ctx context.Context) error
}

type file struct {
	Path string
	Type media_type.MediaType
	Info fs.FileInfo
}

func newFile(path string) (file, error) {
	ret := file{
		Path: path,
	}

	fi, err := os.Stat(path)
	if err != nil {
		return ret, err
	}
	ret.Info = fi

	ret.Type = media_type.GetMediaType(path)

	return ret, nil
}

type task struct {
	Original          file
	OriginalDimension exif.Dimension

	Album      *models.Album
	AlbumIndex int
	AlbumTotal int

	UpdateThumbnail    bool
	ThumbnailDimension exif.Dimension
	Thumbnail          file

	Sidecars    []file
	SidecarXMPs []file

	Media     *models.Media
	MediaURLs map[models.MediaPurpose]*models.MediaURL
}

func newTask(album *models.Album, index, total int, path string) (*task, error) {
	f, err := newFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse %q error: %w", path, err)
	}

	ret := &task{
		Original:   f,
		Album:      album,
		AlbumIndex: index,
		AlbumTotal: total,
	}

	return ret, nil
}

func (t *task) ToExecutableTask() (executableTask, bool) {
	switch {
	case t.Original.Type.IsImage():
		return newPhotoTask(t), true
	}

	return nil, false
}

func (t *task) FindSidecars(ctx context.Context) error {
	ext := path.Ext(t.Original.Path)
	filenamePattern := strings.TrimSuffix(t.Original.Path, ext) + ".*"

	sidecars, err := filepath.Glob(filenamePattern)
	if err != nil {
		return fmt.Errorf("can't find sidecars of %q: %w", t.Original.Path, err)
	}

	for _, sidecar := range sidecars {
		if sidecar == t.Original.Path {
			continue
		}

		sidecarF, err := newFile(sidecar)
		if err != nil {
			continue
		}

		switch {
		case sidecarF.Type == media_type.TypeXMP:
			t.SidecarXMPs = append(t.SidecarXMPs, sidecarF)
			fallthrough
		case sidecarF.Type.IsImage():
			t.Sidecars = append(t.Sidecars, sidecarF)
		default:
			continue
		}
	}

	slices.SortFunc(t.SidecarXMPs, func(a, b file) int {
		ret := a.Info.ModTime().Compare(b.Info.ModTime())
		return 0 - ret // From new to old
	})

	return nil
}

func (t *task) IsValid(ctx context.Context, ignore *ignore.GitIgnore) bool {
	if ignore != nil {
		if ignore.MatchesPath(t.Original.Path) {
			return false
		}
	}

	if t.Original.Type.IsVideo() {
		return true
	}

	if t.Original.Type.IsImage() {
		if utils.EnvDisableRawProcessing.GetBool() {
			return t.Original.Type.IsWebCompatible()
		}

		if t.Original.Type.IsWebCompatible() {
			hasRawSidecar := slices.ContainsFunc(t.Sidecars, func(sidecar file) bool {
				return sidecar.Type.IsImage() && !sidecar.Type.IsWebCompatible()
			})

			return !hasRawSidecar
		}
	}

	return false
}

func (t *task) SyncDimension(ctx context.Context, et *exif.Exiftool) error {
	if err := et.QueryJSONTagsByNumber(t.Original.Path, &t.OriginalDimension); err != nil {
		return fmt.Errorf("can't parse file %q for dimension: %w", t.Original.Path, err)
	}

	scale := float64(t.OriginalDimension.Width()) / 1024
	if hScale := float64(t.OriginalDimension.Height()) / 1024; hScale > scale {
		scale = hScale
	}
	if scale > 1 {
		t.ThumbnailDimension.ImageWidth = uint(float64(t.OriginalDimension.Width()) / scale)
		t.ThumbnailDimension.ImageHeight = uint(float64(t.OriginalDimension.Height()) / scale)
	} else {
		t.ThumbnailDimension.ImageWidth = t.OriginalDimension.Width()
		t.ThumbnailDimension.ImageHeight = t.OriginalDimension.Height()
	}
	t.ThumbnailDimension.Orientation = 1

	return nil
}

func (t *task) SyncMediaAndURLs(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	var media models.Media
	if err := db.
		Preload("media_urls", nil).
		Preload("media_exif", nil).
		Preload("video_metadata", nil).
		Preload("image_face", nil).
		Where("path_hash = ?", models.MD5Hash(t.Original.Path)).First(&media).
		Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			t.Media = &models.Media{
				Title:    path.Base(t.Original.Path),
				Path:     t.Original.Path,
				AlbumID:  t.Album.ID,
				Type:     models.MediaTypePhoto,
				DateShot: t.Original.Info.ModTime(),
			}
			t.UpdateThumbnail = true
			return nil
		}

		return fmt.Errorf("can't query db for %q: %w", t.Original.Path, err)
	}

	t.MediaURLs = make(map[models.MediaPurpose]*models.MediaURL)
	for _, url := range t.MediaURLs {
		t.MediaURLs[url.Purpose] = url
	}

	return nil
}

func (t *task) SyncExif(ctx context.Context, et *exif.Exiftool) error {
	var values struct {
		exif.PhotoMeta
		exif.TimeAll
		exif.MIMEType
		exif.GPS
	}
	if err := et.QueryJSONTagsByNumber(t.Original.Path, &values); err != nil {
		return fmt.Errorf("parse exif error: %w", err)
	}

	values.PhotoMeta.SanitizeFloats()

	exif := models.MediaEXIF{
		Camera:          values.Model,
		Maker:           values.Make,
		Lens:            values.LensModel,
		Iso:             values.ISO,
		Flash:           values.Flash,
		Orientation:     values.Orientation,
		ExposureProgram: values.ExposureProgram,
		Exposure:        values.ExposureTime,
		Aperture:        values.Aperture,
		FocalLength:     values.FocalLength,
		Description:     values.ImageDescription,
	}

	dateShot := values.TimeAll.TimeInLocal()
	if !dateShot.IsZero() {
		exif.DateShot = new(dateShot)
	}

	offsetSec, ok := values.TimeAll.OffsetSecs(dateShot)
	if ok {
		exif.OffsetSecShot = &offsetSec
	}

	if values.GPS.IsValid() {
		exif.GPSLatitude = values.GPS.GPSLatitude
		exif.GPSLongitude = values.GPS.GPSLongitude
	}

	t.Media.Exif = &exif

	return nil
}

func (t *task) DetectFaces(ctx context.Context, recognizer *face.Recognizer) error {
	return nil
}

func (t *task) GenerateBlurHash(ctx context.Context) error {
	if !t.UpdateThumbnail {
		return nil
	}

	f, err := os.Open(t.Thumbnail.Path)
	if err != nil {
		return fmt.Errorf("open thumbnail %q error: %w", t.Thumbnail.Path, err)
	}
	defer f.Close()

	imageData, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("decode thumbnail %q error: %w", t.Thumbnail.Path, err)
	}

	const (
		componentX = 4
		componentY = 3
	)
	hashStr, err := blurhash.Encode(componentX, componentY, imageData)
	if err != nil {
		return fmt.Errorf("encode blurhash %q error: %w", t.Thumbnail.Path, err)
	}

	t.Media.Blurhash = &hashStr

	return nil
}
