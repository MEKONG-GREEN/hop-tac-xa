package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/domain"
	notifyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/db"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/inbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/outbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// fakeDevices là sổ thiết bị giả của pf/sync (gắn token đẩy).
type fakeDevices struct {
	mu   sync.Mutex
	devs map[uuid.UUID]syncport.DeviceInfo
}

func (f *fakeDevices) Device(_ context.Context, _, id uuid.UUID) (syncport.DeviceInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.devs[id]; ok {
		return d, nil
	}
	return syncport.DeviceInfo{}, syncport.ErrDeviceNotFound
}

// fakeMembers là danh bạ Thành viên HTX giả: tài khoản → HTX.
type fakeMembers struct {
	mu      sync.Mutex
	members map[uuid.UUID]uuid.UUID
}

func (f *fakeMembers) IsActiveMember(_ context.Context, _, coop, actor uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.members[actor]
	return ok && c == coop, nil
}

// fakePusher đếm số lần gửi và trả lỗi cấu hình sẵn.
type fakePusher struct {
	mu  sync.Mutex
	n   int
	err error
}

func (p *fakePusher) Name() string { return "fake" }

func (p *fakePusher) Send(context.Context, domain.PushMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n++
	return p.err
}

func (p *fakePusher) sends() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

type member struct {
	f      *fixture
	tid    uuid.UUID
	coop   uuid.UUID
	actor  uuid.UUID
	token  string
	device uuid.UUID
}

type fixture struct {
	t       *testing.T
	iss     *authztest.FakeIssuer
	srv     *httptest.Server
	mod     *notify.Module
	pool    *pgxpool.Pool
	admin   *pgxpool.Pool // đọc thẳng bảng, bỏ qua RLS (kiểm kết quả)
	clk     *clock.Fixed
	devices *fakeDevices
	members *fakeMembers
	tenant  uuid.UUID
}

// newFixture dựng module pf/notify với kênh đẩy p (nil → kênh tắt do chưa cấu hình FCM).
func newFixture(t *testing.T, p channel.Pusher) *fixture {
	t.Helper()
	env := pgtest.Get(t)
	f := &fixture{
		t: t, iss: authztest.New(t), pool: env.Pool(t, migrations.PF, dbadmin.AppPlatform), admin: env.AdminPool(t, migrations.PF),
		clk: clock.NewFixed(time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)), tenant: ids.New(),
		devices: &fakeDevices{devs: map[uuid.UUID]syncport.DeviceInfo{}}, members: &fakeMembers{members: map[uuid.UUID]uuid.UUID{}},
	}
	if p == nil {
		p = channel.DisabledPusher{Reason: channel.ReasonFCMNotConfigured}
	}
	f.mod = notify.New(f.pool, f.clk, notify.Deps{Devices: f.devices, Pusher: p})
	f.mod.UseMembers(f.members)
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	f.mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

// newMember tạo Thành viên HTX (vai htx_member, AH-39) của HTX coop, có một thiết bị đã đăng ký.
func (f *fixture) newMember(coop uuid.UUID) member {
	actor := ids.New()
	f.members.mu.Lock()
	f.members.members[actor] = coop
	f.members.mu.Unlock()
	dev := ids.New()
	f.devices.mu.Lock()
	f.devices.devs[dev] = syncport.DeviceInfo{ID: dev, ActorRef: actor, Platform: "android"}
	f.devices.mu.Unlock()
	return member{f: f, tid: f.tenant, coop: coop, actor: actor, device: dev,
		token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: f.tenant, Roles: []string{partyport.RoleHTXMember}, OrgRef: coop, MFA: true})}
}

// newAdmin tạo tài khoản quản trị nền tảng (không phải Thành viên HTX).
func (f *fixture) newAdmin() member {
	actor := ids.New()
	return member{f: f, tid: f.tenant, actor: actor,
		token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: f.tenant, Roles: []string{authz.RolePlatformAdmin}, MFA: true})}
}

type resp struct {
	status int
	body   map[string]any
}

func (m member) do(method, path string, body any) resp {
	m.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, m.f.srv.URL+path, rd)
	if err != nil {
		m.f.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		req.Header.Set(idempotency.Header, ids.New().String())
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		m.f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, body: map[string]any{}}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (r resp) items() []map[string]any {
	var out []map[string]any
	list, _ := r.body["items"].([]any)
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (r resp) str(k string) string { s, _ := r.body[k].(string); return s }

func (r resp) num(k string) int { n, _ := r.body[k].(float64); return int(n) }

func (m member) list(query string) []map[string]any {
	m.f.t.Helper()
	r := m.do(http.MethodGet, "/v1/notifications"+query, nil)
	if r.status != http.StatusOK {
		m.f.t.Fatalf("list: %d %v", r.status, r.body)
	}
	return r.items()
}

func (m member) unread() int {
	m.f.t.Helper()
	r := m.do(http.MethodGet, "/v1/notifications/unread-count", nil)
	if r.status != http.StatusOK {
		m.f.t.Fatalf("unread-count: %d %v", r.status, r.body)
	}
	return r.num("unread")
}

// registerToken gắn token đẩy riêng cho thiết bị của thành viên (token trùng ở thiết bị khác bị thay thế).
func (m member) registerToken() {
	m.f.t.Helper()
	r := m.do(http.MethodPost, "/v1/devices/"+m.device.String()+"/push-token", map[string]string{"provider": "fcm", "token": "tok-" + m.device.String() + strings.Repeat("a", 8)})
	if r.status != http.StatusOK {
		m.f.t.Fatalf("push-token: %d %v", r.status, r.body)
	}
}

func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.admin.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

type delivery struct {
	status    string
	lastError string
	sent      bool
}

// deliveries đọc các lần gửi đẩy của một thông báo.
func (f *fixture) deliveries(notificationID string) []delivery {
	f.t.Helper()
	rows, err := f.admin.Query(context.Background(),
		`SELECT status, COALESCE(last_error, ''), sent_at IS NOT NULL FROM pf_notify.push_delivery WHERE notification_id = $1`, notificationID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []delivery
	for rows.Next() {
		var d delivery
		if err := rows.Scan(&d.status, &d.lastError, &d.sent); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

// pushEnv dựng phong bì pf.notify.push.requested cho lô gửi đẩy của một thông báo, như outbox giao cho worker.
func (f *fixture) pushEnv(notificationID string) outbox.Envelope {
	f.t.Helper()
	var batch uuid.UUID
	if err := f.admin.QueryRow(context.Background(),
		`SELECT batch_id FROM pf_notify.push_delivery WHERE notification_id = $1 LIMIT 1`, notificationID).Scan(&batch); err != nil {
		f.t.Fatal(err)
	}
	payload, _ := json.Marshal(notifyport.PushRequestedV1{BatchID: batch, Kind: notifyport.KindSeasonTaskDue, Deliveries: 1})
	return outbox.Envelope{EventID: ids.New(), EventType: notifyport.EventPushRequested, SchemaVersion: 1, TenantID: f.tenant,
		OccurredAt: f.clk.Now(), Producer: notifyport.EventProducerName, Payload: payload}
}

// runPush chạy consumer gửi đẩy cho phong bì env (cùng phong bì chạy lại = giao lặp).
func (f *fixture) runPush(env outbox.Envelope) (bool, error) {
	return inbox.Process(context.Background(), db.ForModule(f.pool, notify.DBModule), notifyport.ConsumerPush, env, f.mod.PushHandler())
}

// taskDue tạo NTF.2 cho các người nhận qua cổng.
func (f *fixture) taskDue(coop uuid.UUID, recipients ...uuid.UUID) (notifyport.TaskDueNotice, error) {
	n := notifyport.TaskDueNotice{
		MemberNoticeBase: notifyport.MemberNoticeBase{SourceID: ids.New(), CooperativeRef: coop, Recipients: recipients},
		SeasonRef:        ids.New(), TaskRef: ids.New(), HouseholdCode: "NH001", TaskName: "Bón phân", DueDate: "2026-10-02",
	}
	return n, f.mod.MemberNotifier().TaskDue(context.Background(), f.tenant, n)
}
