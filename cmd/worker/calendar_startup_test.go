package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
)

func TestCalendarNoticeRequiresDirectoryBeforeConsuming(t *testing.T) {
	m := cooperative.New(nil, clock.System{}, cooperative.Deps{})
	if err := m.CalendarNoticeReady(); err == nil {
		t.Fatal("missing directory accepted")
	}
	t.Setenv("AG_DB_URL", "")
	for _, consumer := range wiredConsumers(slog.New(slog.NewTextHandler(io.Discard, nil))) {
		if consumer.name == port.CalendarNoticeConsumer {
			if consumer.prepare == nil {
				t.Fatal("calendar consumer has no startup preparation")
			}
			if err := consumer.prepare(context.Background()); err == nil || !strings.Contains(err.Error(), "AG_DB_URL") {
				t.Fatalf("startup missing DB=%v", err)
			}
			return
		}
	}
	t.Fatal("calendar consumer not wired")
}
