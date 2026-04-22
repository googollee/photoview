package image

import (
	"context"
	"fmt"

	"github.com/photoview/photoview/api/globalinit"
	"github.com/photoview/photoview/api/log"
	"gopkg.in/gographics/imagick.v3/imagick"
)

func init() {
	globalinit.Register("imagick", func(ctx context.Context) error {
		imagick.Initialize()
		verstr, vernum := imagick.GetVersion()
		log.Info(ctx, "ImagickWand", "version", verstr, "number", vernum)

		return nil
	}, func(context.Context) {
		imagick.Terminate()
	})
}

type Image struct {
	wand *imagick.MagickWand
}

func New(inputPath string) (*Image, error) {
	wand := imagick.NewMagickWand()

	if err := wand.ReadImage(inputPath); err != nil {
		return nil, fmt.Errorf("ImagickWand read %q error: %w", inputPath, err)
	}

	if err := wand.AutoOrientImage(); err != nil {
		return nil, fmt.Errorf("ImagickWand auto-orient %q error: %w", inputPath, err)
	}

	// Reset EXIF orientation to 1 (top-left) since image is now properly oriented
	if err := wand.SetImageOrientation(imagick.ORIENTATION_TOP_LEFT); err != nil {
		return nil, fmt.Errorf("ImagickWand set orientation for %q error: %w", inputPath, err)
	}

	return &Image{
		wand: wand,
	}, nil
}

func (i *Image) Close() {
	i.wand.Destroy()
}

func (i *Image) EncodeJpeg(jpegQuality uint) error {
	if err := i.wand.SetFormat("JPEG"); err != nil {
		return fmt.Errorf("ImagickWand set JPEG format error: %w", err)
	}

	if err := i.wand.SetImageCompressionQuality(jpegQuality); err != nil {
		return fmt.Errorf("ImagickWand set JPEG quality %d error: %w", jpegQuality, err)
	}

	return nil
}

func (i *Image) Save(outputPath string) error {
	if err := i.wand.WriteImage(outputPath); err != nil {
		return fmt.Errorf("ImagickWand write %q error: %w", outputPath, err)
	}

	return nil
}

func (i *Image) GenerateThumbnail(width, height uint) error {
	if err := i.wand.ThumbnailImage(width, height); err != nil {
		return fmt.Errorf("ImagickWand generate thumbnail error: %w", err)
	}

	return nil
}
