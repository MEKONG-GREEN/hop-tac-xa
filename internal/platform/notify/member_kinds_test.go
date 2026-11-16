package notify

import (
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
)

// AH-22: bốn kind của thông báo Thành viên HTX là kind native — module khác không đăng ký lại được.
func TestAH22_MemberKindsAreNative(t *testing.T) {
	for _, k := range []string{port.KindSeasonCalendarPublished, port.KindSeasonTaskDue, port.KindSeasonLogMissing, port.KindSeasonProcessChanged} {
		t.Run(k, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("kind native bị đăng ký lại mà không lỗi")
				}
			}()
			New(nil, clock.System{}, Deps{Kinds: []port.KindRegistration{{Kinds: []string{k}}}})
		})
	}
}
