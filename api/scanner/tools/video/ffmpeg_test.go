package video

import (
	"errors"
	"regexp"
	"testing"

	"gopkg.in/vansante/go-ffprobe.v2"

	"github.com/photoview/photoview/api/scanner/tools"
	"github.com/photoview/photoview/api/test_utils"
	"github.com/photoview/photoview/api/utils"
)

const testdataBinPath = "./test_data/mock_bin"

func TestFfmpegNotExist(t *testing.T) {
	test_utils.SetPathWithCurrent(t, "")

	if got, want := initFFMpeg(t.Context()), tools.ErrInvalid; !errors.Is(got, want) {
		t.Errorf("initFFMpeg() = %v, want: %v", got, want)
	}

	if got, want := EncodeVideo("input", "output"), tools.ErrInvalid; !errors.Is(got, want) {
		t.Errorf("Ffmpge.EncodeMp4() = %v, want: %v", got, want)
	}

	if got, want := EncodeVideoThumbnail("input", "output", nil), tools.ErrInvalid; !errors.Is(got, want) {
		t.Errorf("Ffmpge.EncodeMp4() = %v, want: %v", got, want)
	}
}

func TestFfmpegVersionFail(t *testing.T) {
	test_utils.SetPathWithCurrent(t, testdataBinPath)
	t.Setenv("FAIL_WITH", "expect failure")

	if got, want := initFFMpeg(t.Context()), tools.ErrInvalid; !errors.Is(got, want) {
		t.Errorf("initFFMpeg() = %v, want: %v", got, want)
	}

	if got, want := EncodeVideo("input", "output"), tools.ErrInvalid; !errors.Is(got, want) {
		t.Errorf("Ffmpge.EncodeMp4() = %v, want: %v", got, want)
	}

	if got, want := EncodeVideoThumbnail("input", "output", nil), tools.ErrInvalid; !errors.Is(got, want) {
		t.Errorf("Ffmpge.EncodeMp4() = %v, want: %v", got, want)
	}
}

func TestFfmpegIgnore(t *testing.T) {
	test_utils.SetPathWithCurrent(t, testdataBinPath)
	t.Setenv("PHOTOVIEW_DISABLE_VIDEO_ENCODING", "true")

	if got := initFFMpeg(t.Context()); got != nil {
		t.Errorf("initFFMpeg() = %v, want: nil", got)
	}

	if got, want := EncodeVideo("input", "output"), tools.ErrDisabled; !errors.Is(got, want) {
		t.Errorf("Ffmpge.EncodeMp4() = %v, want: %v", got, want)
	}

	if got, want := EncodeVideoThumbnail("input", "output", nil), tools.ErrDisabled; !errors.Is(got, want) {
		t.Errorf("Ffmpge.EncodeMp4() = %v, want: %v", got, want)
	}
}

func TestFfmpeg(t *testing.T) {
	test_utils.SetPathWithCurrent(t, testdataBinPath)

	if got := initFFMpeg(t.Context()); got != nil {
		t.Errorf("initFFMpeg() = %v, want: nil", got)
	}

	t.Run("EncodeVideoFailed", func(t *testing.T) {
		t.Setenv("FAIL_WITH", "expect failure")

		err := EncodeVideo("input", "output")
		if err == nil {
			t.Fatalf("Ffmpeg.EncodeMp4(...) = nil, should be an error.")
		}
		if got, want := err.Error(), `^encoding video with ".*/test_data/mock_bin/ffmpeg" \[-i input -vcodec h264 .* output\] error: .*$`; !regexp.MustCompile(want).MatchString(got) {
			t.Errorf("Ffmpeg.EncodeMp4(...) = %q, should be as reg pattern %q", got, want)
		}
	})

	t.Run("EncodeVideoSucceeded", func(t *testing.T) {
		err := EncodeVideo("input", "output")
		if err != nil {
			t.Fatalf("Ffmpeg.EncodeMp4(...) = %v, should be nil.", err)
		}
	})

	probeData := &ffprobe.ProbeData{
		Format: &ffprobe.Format{
			DurationSeconds: 10,
		},
	}
	t.Run("EncodeVideoThumbnailMp4Failed", func(t *testing.T) {
		t.Setenv("FAIL_WITH", "expect failure")

		err := EncodeVideoThumbnail("input", "output", probeData)
		if err == nil {
			t.Fatalf("Ffmpeg.EncodeVideoThumbnail(...) = nil, should be an error.")
		}
		if got, want := err.Error(), `^encoding video thumbnail with ".*/test_data/mock_bin/ffmpeg" \[-ss 2 -i input .* output\] error: .*$`; !regexp.MustCompile(want).MatchString(got) {
			t.Errorf("Ffmpeg.EncodeVideoThumbnail(...) = %q, should be as reg pattern %q", got, want)
		}
	})

	t.Run("EncodeVideoThumbnailSucceeded", func(t *testing.T) {
		err := EncodeVideoThumbnail("input", "output", probeData)
		if err != nil {
			t.Fatalf("Ffmpeg.EncodeVideoThumbnail(...) = %v, should be nil.", err)
		}
	})
}

func TestFfmpegWithHWAcc(t *testing.T) {
	test_utils.SetPathWithCurrent(t, testdataBinPath)
	t.Setenv(utils.EnvVideoHardwareAcceleration.GetName(), "qsv")

	if got := initFFMpeg(t.Context()); got != nil {
		t.Errorf("initFFMpeg() = %v, want: nil", got)
	}

	t.Setenv("FAIL_WITH", "expect failure")

	err := EncodeVideo("input", "output")
	if err == nil {
		t.Fatalf("Ffmpeg.EncodeMp4(...) = nil, should be an error.")
	}
	if got, want := err.Error(), `^encoding video with ".*/test_data/mock_bin/ffmpeg" \[-i input -vcodec h264_qsv .* output\] error: .*$`; !regexp.MustCompile(want).MatchString(got) {
		t.Errorf("Ffmpeg.EncodeMp4(...) = %q, should be as reg pattern %q", got, want)
	}
}

func TestFfmpegWithCustomCodec(t *testing.T) {
	test_utils.SetPathWithCurrent(t, testdataBinPath)
	t.Setenv(utils.EnvVideoHardwareAcceleration.GetName(), "_custom")

	if got := initFFMpeg(t.Context()); got != nil {
		t.Errorf("initFFMpeg() = %v, want: nil", got)
	}

	t.Setenv("FAIL_WITH", "expect failure")

	err := EncodeVideo("input", "output")
	if err == nil {
		t.Fatalf("Ffmpeg.EncodeMp4(...) = nil, should be an error.")
	}
	if got, want := err.Error(), `^encoding video with ".*/test_data/mock_bin/ffmpeg" \[-i input -vcodec custom .* output\] error: .*$`; !regexp.MustCompile(want).MatchString(got) {
		t.Errorf("Ffmpeg.EncodeMp4(...) = %q, should be as reg pattern %q", got, want)
	}
}
