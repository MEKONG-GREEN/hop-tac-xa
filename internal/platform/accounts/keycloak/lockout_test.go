package keycloak

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
)

// DefaultLockoutWait phải khớp thời gian khóa tạm của các realm (P0-VPS-OPS #131) — nếu không, mốc
// "khóa tự hết lúc" trên màn Admin (ADMIN-UNLOCK #42/#61) sẽ sai.
func TestDefaultLockoutWaitMatchesRealms(t *testing.T) {
	for _, f := range []string{"agrigreen-realm.json", "agrigreen-kn-admin-realm.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "compose", "keycloak", f))
		if err != nil {
			t.Fatal(err)
		}
		var r struct {
			PermanentLockout      bool `json:"permanentLockout"`
			WaitIncrementSeconds  int  `json:"waitIncrementSeconds"`
			MaxFailureWaitSeconds int  `json:"maxFailureWaitSeconds"`
			FailureFactor         int  `json:"failureFactor"`
			MaxDeltaTimeSeconds   int  `json:"maxDeltaTimeSeconds"`
			QuickLoginCheckMillis int  `json:"quickLoginCheckMilliSeconds"`
			MinQuickLoginWaitSecs int  `json:"minimumQuickLoginWaitSeconds"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		want := time.Duration(r.MaxFailureWaitSeconds) * time.Second
		if r.PermanentLockout || r.WaitIncrementSeconds != r.MaxFailureWaitSeconds || want != DefaultLockoutWait {
			t.Errorf("%s: permanentLockout=%v waitIncrement=%d maxFailureWait=%d, DefaultLockoutWait=%v",
				f, r.PermanentLockout, r.WaitIncrementSeconds, r.MaxFailureWaitSeconds, DefaultLockoutWait)
		}
		// FN-01: bộ đếm của domain (mã định danh không tồn tại, số lần thử còn lại) phải khớp chính sách realm.
		if r.FailureFactor != domain.LoginMaxFailures || want != domain.LoginLockout ||
			time.Duration(r.MaxDeltaTimeSeconds)*time.Second != domain.LoginFailureWindow ||
			time.Duration(r.QuickLoginCheckMillis)*time.Millisecond != domain.LoginQuickCheck ||
			time.Duration(r.MinQuickLoginWaitSecs)*time.Second != domain.LoginQuickWait || domain.LoginQuickWait != DefaultQuickLoginWait {
			t.Errorf("%s: failureFactor=%d maxDelta=%d quickCheck=%dms quickWait=%ds lệch với domain.Login*",
				f, r.FailureFactor, r.MaxDeltaTimeSeconds, r.QuickLoginCheckMillis, r.MinQuickLoginWaitSecs)
		}
	}
}
