package obs

import (
	"errors"
	"log/slog"
	"testing"
)

func TestBusValue(t *testing.T) {
	if v := busValue(slog.String("setup_token", "s3cret")); v != "[redacted]" {
		t.Fatalf("token: %v", v)
	}
	if v := busValue(slog.Any("err", errors.New("boom"))); v != "boom" {
		t.Fatalf("err: %v", v)
	}
	if v := busValue(slog.Int("n", 3)); v != int64(3) {
		t.Fatalf("int: %#v", v)
	}
}
