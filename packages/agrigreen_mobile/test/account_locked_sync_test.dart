import 'dart:typed_data';

import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

/// FN-01/EX-06 (AH-301): tài khoản bị khóa trong lúc đang dùng → lượt đồng bộ
/// dừng ở trạng thái cần đăng nhập, mọi dữ liệu chờ gửi giữ nguyên; mở khóa thì
/// gửi tiếp được.
void main() {
  late Harness h;
  setUp(() async => h = await Harness.create());
  tearDown(() => h.dispose());

  Future<void> expectLockedStatus() async {
    final st = await h.sync.status();
    expect(st.state, SyncState.needsLogin);
    expect(st.lastError, AccountLockedException.accountLockedCode);
  }

  test(
    'AC-N2: ảnh và bản ghi chờ gửi giữ nguyên, ảnh không bị đánh dấu hỏng',
    () async {
      await h.evidence.addSealed(
        id: 'f1',
        subjectRef: 'h1',
        envelope: SealedEnvelope(
          meta: const {'captured_at': '2026-12-02T08:00:00+07:00'},
          ciphertext: Uint8List(10),
        ),
      );
      await h.outbox.enqueue(
        entity: PushEntity.fieldReport,
        payload: {'note': 'n1', 'plot_id': 'p1'},
      );
      h.fake.accountLocked = true;

      expect(await h.sync.syncOnce(force: true), isNull);
      await expectLockedStatus();
      expect((await h.evidence.byId('f1'))!.status, 'pending');
      expect((await h.outbox.counts()).pending, 1);

      h.fake.accountLocked = false;
      expect(await h.sync.syncOnce(force: true), isNotNull);
      expect((await h.evidence.byId('f1'))!.status, 'uploaded');
      expect((await h.outbox.counts()).pending, 0);
      expect(h.fake.ledger, hasLength(1));
    },
  );

  test(
    'AC-N2: lệnh trong hàng đợi không bị bỏ khi tài khoản bị khóa',
    () async {
      final a = await h.actions.enqueue(
        ApiActionKind.notificationRead,
        target: 'n1',
      );
      h.fake.accountLocked = true;

      expect(await h.sync.syncOnce(force: true), isNull);
      await expectLockedStatus();
      expect((await h.actions.byId(a.id))!.status, ApiActionStatus.pending);

      h.fake.accountLocked = false;
      expect(await h.sync.syncOnce(force: true), isNotNull);
      expect((await h.actions.byId(a.id))!.status, ApiActionStatus.done);
    },
  );
}
