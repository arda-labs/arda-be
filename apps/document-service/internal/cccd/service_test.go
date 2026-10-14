package cccd

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"slices"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/document-service/internal/extractor"
)

type fakeLLM struct {
	replies []string
	errs    []error
	calls   int
	images  [][][]byte
	block   bool
}

func (f *fakeLLM) Complete(ctx context.Context, images [][]byte, prompt string) (string, error) {
	f.calls++
	f.images = append(f.images, images)
	if f.block {
		<-ctx.Done()
		return "", extractor.ErrModelTimeout
	}
	i := min(f.calls-1, len(f.replies)-1)
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	return f.replies[i], err
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 40, 25))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const goodReply = "```json\n" + `{"card_version":"cccd_chip_2021","image_quality":"ok","id_number":"082088123456",
"full_name":"HOÀNG THÙY HUYỀN","date_of_birth":"26/05/1988","sex":"Nam","nationality":"Việt Nam",
"place_of_origin":"Xã Điện Dương, Thành phố Huế, Thừa Thiên Huế","residence":"Số 181 Võ Nguyên Giáp, Hưng Yên",
"issue_date":"10/07/2021","issue_place":"CỤC TRƯỞNG CỤC CẢNH SÁT QUẢN LÝ HÀNH CHÍNH VỀ TRẬT TỰ XÃ HỘI",
"expiry_date":"26/05/2028"}` + "\n```"

func fixedNow() time.Time { return time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC) }

func TestExtractHappyPath(t *testing.T) {
	llm := &fakeLLM{replies: []string{goodReply}}
	svc := NewService(llm, "qwen3.8-flash", "cloud", 1600, fixedNow)
	res, err := svc.Extract(context.Background(), pngBytes(t), pngBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.DocType != "cccd" || res.CardVersion != CardChip2021 || res.Model != "qwen3.8-flash" || res.ModelLocation != "cloud" {
		t.Fatalf("meta = %+v", res)
	}
	if *res.Fields.IDNumber.Value != "082088123456" || *res.Fields.DateOfBirth.Value != "1988-05-26" || *res.Fields.Sex.Value != "M" {
		t.Fatalf("fields = %+v", res.Fields)
	}
	if res.Warnings == nil || len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	res.Fields.Each(func(name string, f Field) {
		if len(f.Flags) != 0 {
			t.Errorf("%s flags = %v", name, f.Flags)
		}
	})
	if llm.calls != 1 || len(llm.images[0]) != 2 || extractor.SniffImageType(llm.images[0][0]) != "image/jpeg" {
		t.Fatalf("calls=%d images=%d", llm.calls, len(llm.images[0]))
	}
}

func TestExtractRetriesOnceOnUnusableOutput(t *testing.T) {
	echo := `[{"type": "object", "properties": {"so_cccd": {"type": "string"}}}]`
	llm := &fakeLLM{replies: []string{echo, goodReply}}
	if _, err := NewService(llm, "m", "cloud", 1600, fixedNow).Extract(context.Background(), pngBytes(t), nil); err != nil {
		t.Fatal(err)
	}
	if llm.calls != 2 {
		t.Fatalf("calls = %d", llm.calls)
	}

	llm = &fakeLLM{replies: []string{"xin lỗi, tôi không đọc được"}}
	_, err := NewService(llm, "m", "cloud", 1600, fixedNow).Extract(context.Background(), pngBytes(t), nil)
	if !errors.Is(err, ErrBadModelOutput) || llm.calls != 2 {
		t.Fatalf("err=%v calls=%d", err, llm.calls)
	}
}

func TestExtractNotCCCD(t *testing.T) {
	llm := &fakeLLM{replies: []string{`{"card_version":"not_cccd"}`}}
	if _, err := NewService(llm, "m", "cloud", 1600, fixedNow).Extract(context.Background(), pngBytes(t), nil); !errors.Is(err, ErrNotCCCD) {
		t.Fatalf("err = %v", err)
	}
}

func TestExtractModelErrorNotRetried(t *testing.T) {
	llm := &fakeLLM{replies: []string{""}, errs: []error{extractor.ErrModelUnavailable}}
	_, err := NewService(llm, "m", "cloud", 1600, fixedNow).Extract(context.Background(), pngBytes(t), nil)
	if !errors.Is(err, extractor.ErrModelUnavailable) || llm.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, llm.calls)
	}
}

// Review focus 2: garbage first, then the deadline expires during the retry.
func TestExtractRetryHitsDeadline(t *testing.T) {
	llm := &fakeLLM{replies: []string{"garbage"}}
	svc := NewService(llm, "m", "cloud", 1600, fixedNow)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	first := true
	wrapped := completerFunc(func(c context.Context, im [][]byte, p string) (string, error) {
		if first {
			first = false
			return llm.Complete(c, im, p)
		}
		<-c.Done()
		return "", extractor.ErrModelTimeout
	})
	svc.llm = wrapped
	if _, err := svc.Extract(ctx, pngBytes(t), nil); !errors.Is(err, extractor.ErrModelTimeout) {
		t.Fatalf("err = %v", err)
	}
}

type completerFunc func(context.Context, [][]byte, string) (string, error)

func (f completerFunc) Complete(c context.Context, im [][]byte, p string) (string, error) {
	return f(c, im, p)
}

// Review focus 1: id_number as a JSON number loses its leading zero.
func TestExtractNumericIDNumber(t *testing.T) {
	llm := &fakeLLM{replies: []string{`{"card_version":"cccd_chip_2021","id_number":82279855585,"sex":"Nam","date_of_birth":"26/05/1988"}`}}
	res, err := NewService(llm, "m", "cloud", 1600, fixedNow).Extract(context.Background(), pngBytes(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if *res.Fields.IDNumber.Value != "82279855585" || !slices.Contains(res.Fields.IDNumber.Flags, FlagInvalidFormat) {
		t.Fatalf("id = %+v", res.Fields.IDNumber)
	}
	if !slices.Contains(res.Fields.FullName.Flags, FlagNotFound) {
		t.Fatalf("missing key must be not_found: %+v", res.Fields.FullName)
	}
}

func TestExtractWarnings(t *testing.T) {
	llm := &fakeLLM{replies: []string{`{"card_version":"cccd_vip","image_quality":"low"}`}}
	res, err := NewService(llm, "m", "cloud", 1600, fixedNow).Extract(context.Background(), pngBytes(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{WarnBackSideMissing, WarnCardVersionUnknown, WarnImageQualityLow}
	if !slices.Equal(res.Warnings, want) || res.CardVersion != CardUnknown || len(llm.images[0]) != 1 {
		t.Fatalf("warnings = %v version = %s", res.Warnings, res.CardVersion)
	}
}

func TestExtractRejectsBadImage(t *testing.T) {
	llm := &fakeLLM{replies: []string{goodReply}}
	if _, err := NewService(llm, "m", "cloud", 1600, fixedNow).Extract(context.Background(), []byte("GIF89a"), nil); !errors.Is(err, extractor.ErrUnsupportedImage) {
		t.Fatalf("err = %v", err)
	}
	if llm.calls != 0 {
		t.Fatal("model must not be called for a bad image")
	}
}

func TestTodayUsesVietnamDate(t *testing.T) {
	// 2026-10-13 18:30 UTC is already 2026-10-14 in Vietnam.
	svc := NewService(nil, "m", "cloud", 1600, func() time.Time { return time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC) })
	if got := svc.today(); !got.Equal(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("today = %v", got)
	}
}
