package process

import (
	"context"

	"github.com/photoview/photoview/api/graphql/models"
	"github.com/photoview/photoview/api/log"
	"github.com/photoview/photoview/api/scanner/tools/exif"
)

type photoProcessor struct {
	*Processor
}

func (p *photoProcessor) Process(ctx context.Context, task *task) error {

}

func (p *photoProcessor) SyncExif(ctx context.Context, task *task) {
	var values struct {
		exif.PhotoMeta
		exif.TimeAll
		exif.MIMEType
		exif.GPS
	}
	if err := p.exif.QueryJSONTagsByNumber(task.Original.Path, &values); err != nil {
		log.Warn(ctx, "query exif data error", "error", err)
		return
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

	task.Media.Exif = &exif
}
