package assembly

import (
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	krefport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/kref/port"
	mrvport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	repport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/reports/port"
	wwport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/inbox"
)

// AuditConsentConsumer là consumer mẫu G0 của worker (ghi nhận sự kiện đồng ý).
const AuditConsentConsumer = "pf.audit.consent"

// Queue là hàng đợi quorum của một consumer (tên = tên consumer) cùng các event_type bind vào.
type Queue struct {
	Name   string
	Events []string
}

// Queues là TOÀN BỘ hàng đợi consumer của cụm pf: consumer của cmd/worker (G1, pf/notify,
// pf/reports, liên module G2 ở Consumers).
//
// outbox-publisher khai báo các hàng đợi này (DeclareQueues) TRƯỚC khi phát thông điệp đầu
// tiên, để sự kiện phát ra trước lần khởi động đầu của một consumer không bị exchange
// ag.events bỏ đi vì chưa có hàng đợi nào bind (docs/runbooks/rabbitmq-queues.md). Kiểm thử
// của cmd/worker đòi mọi consumer của worker có trong danh sách với đúng các sự kiện.
// Liệt kê không cần DB.
func Queues() []Queue {
	out := []Queue{
		{Name: AuditConsentConsumer, Events: []string{partyport.EventConsentRecorded}},
		{Name: mrvport.ExportConsumer, Events: []string{mrvport.EventExportRequested}},
		{Name: accport.OrgStatusConsumer, Events: accounts.OrgStatusEvents},
		{Name: accport.PasswordResetConsumer, Events: []string{accport.EventPasswordResetRequested}}, // #255 ADR-0057
		{Name: wwport.BuildConsumer, Events: []string{wwport.EventBuildRequested}},
		{Name: repport.ExportConsumer, Events: []string{repport.EventExportRequested}},
		{Name: repport.DeliveryConsumer, Events: []string{repport.EventExportCompleted}},
		{Name: repport.DispatchConsumer, Events: []string{repport.EventDispatchRequested}}, // KN v7.7 P4: báo cáo Chi cục
		{Name: krefport.Consumer, Events: krefport.Events},                                 // W2.5: bản chiếu k_ref
	}
	for _, c := range notify.New(nil, clock.System{}, notify.Deps{}).Consumers() {
		out = append(out, Queue{Name: c.Name, Events: c.Events})
	}
	for _, c := range Consumers {
		out = append(out, Queue{Name: c.Name, Events: c.Events})
	}
	return out
}

// DeclareQueues khai báo (idempotent) mọi hàng đợi của Queues cùng DLQ và binding —
// cùng tham số với inbox.DeclareQueue mà worker/optimizer dùng khi khởi động.
func DeclareQueues(ch *amqp.Channel) error {
	for _, q := range Queues() {
		if err := inbox.DeclareQueue(ch, q.Name, q.Events); err != nil {
			return err
		}
	}
	return nil
}
