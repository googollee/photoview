package video

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/photoview/photoview/api/globalinit"
	"github.com/photoview/photoview/api/log"
	"github.com/photoview/photoview/api/scanner/tools"
	"github.com/photoview/photoview/api/utils"
	"gopkg.in/vansante/go-ffprobe.v2"
)

const defaultCodec = "h264"

var hwAccToCodec = map[string]string{
	"qsv":   defaultCodec + "_qsv",
	"vaapi": defaultCodec + "_vaapi",
	"nvenc": defaultCodec + "_nvenc",
}

var (
	ffmpegPath    string
	ffmpegVersion string
	ffmpegError   error = tools.ErrDisabled
	ffmpegCodec   string
)

func init() {
	globalinit.Register("ffmpeg", initFFMpeg, nil)
	globalinit.Register("ffprobe", initFFProbe, nil)
}

func initFFMpeg(ctx context.Context) error {
	if utils.EnvDisableVideoEncoding.GetBool() {
		log.Warn(ctx, "video encoding is disabled", utils.EnvDisableVideoEncoding.GetName(), utils.EnvDisableVideoEncoding.GetValue())
		ffmpegError = tools.ErrDisabled
		return nil
	}

	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		ffmpegError = fmt.Errorf("find ffmpeg binary error: %w: %w", tools.ErrInvalid, err)
		return ffmpegError
	}
	ffmpegPath = path

	version, err := exec.Command(path, "-version").Output()
	if err != nil {
		ffmpegError = fmt.Errorf("run `ffmpeg -version` error: %w: %w", tools.ErrInvalid, err)
		return ffmpegError
	}
	ffmpegVersion = string(bytes.Split(version, []byte("\n"))[0])
	ffmpegError = nil

	hwAcc := utils.EnvVideoHardwareAcceleration.GetValue()
	codec, ok := hwAccToCodec[hwAcc]
	if !ok {
		if strings.HasPrefix(hwAcc, "_") {
			// A secret way to set the codec directly.
			codec = hwAcc[1:]
		} else {
			log.Warn(ctx, "invalid codec", utils.EnvVideoHardwareAcceleration.GetName(), utils.EnvDisableVideoEncoding.GetValue())
			codec = defaultCodec
		}
	}
	ffmpegCodec = codec

	log.Info(ctx, "ffmpeg cli", "cli", ffmpegPath, "version", ffmpegVersion, "codec", ffmpegCodec)

	return nil
}
func initFFProbe(ctx context.Context) error {
	if utils.EnvDisableVideoEncoding.GetBool() {
		// initFFMpeg prints warning.
		return nil
	}

	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return fmt.Errorf("find ffprobe cli error: %w: %w", tools.ErrInvalid, err)
	}

	version, err := exec.Command(path, "-version").Output()
	if err != nil {
		return fmt.Errorf("run `ffprobe -version` error: %w: %w", tools.ErrInvalid, err)
	}

	ffprobe.SetFFProbeBinPath(path)

	log.Info(ctx, "ffprobe cli", "cli", path, "version", version)

	return nil
}

func EncodeVideo(inputPath string, outputPath string) error {
	if ffmpegError != nil {
		return fmt.Errorf("encoding video %q error: ffmpeg: %w", inputPath, ffmpegError)
	}

	args := []string{
		"-i",
		inputPath,
		"-vcodec", ffmpegCodec,
		"-acodec", "aac",
		"-vf", "scale='min(1080,iw)':'min(1080,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2",
		"-movflags", "+faststart+use_metadata_tags",
		outputPath,
	}

	cmd := exec.Command(ffmpegPath, args...)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("encoding video with %q %v error: %w", ffmpegPath, args, err)
	}

	return nil
}

func EncodeVideoThumbnail(inputPath string, outputPath string, probeData *ffprobe.ProbeData) error {
	if ffmpegError != nil {
		return fmt.Errorf("encoding video %q error: ffmpeg: %w", inputPath, ffmpegError)
	}

	thumbnailOffsetSeconds := fmt.Sprintf("%.f", probeData.Format.DurationSeconds*0.25)

	args := []string{
		"-ss", thumbnailOffsetSeconds, // grab frame at time offset
		"-i",
		inputPath,
		"-vframes", "1", // output one frame
		"-an", // disable audio
		"-vf", "scale='min(1024,iw)':'min(1024,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2",
		outputPath,
	}

	cmd := exec.Command(ffmpegPath, args...)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("encoding video thumbnail with %q %v error: %w", ffmpegPath, args, err)
	}

	return nil
}
