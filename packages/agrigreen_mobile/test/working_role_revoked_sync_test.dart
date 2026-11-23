import 'dart:typed_data';

import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/crypto/envelope.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/harness.dart';

/// FN-TK-01/EX-03 (AH-222): vai làm việc bị ngừng trong lúc đang dùng → lượt
/// đồng bộ dừng, báo mã để app đưa về màn Chọn vai trò; ảnh, bản ghi và lệnh
/// chờ gửi giữ nguyên, không bị đánh dấu hỏng hay bỏ.
void main() {
  late Harness h;
  setUp(() async => h = await Harness.create());
  tearDown(() => h.dispose());

  test('dữ liệu chờ gửi giữ nguyên; chọn lại vai thì gửi tiếp', () async {
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
    final a = await h.actions.enqueue(
      ApiActionKind.notificationRead,
      target: 'n1',
    );
    h.fake.workingRoleRevoked = true;

    expect(await h.sync.syncOnce(force: true), isNull);
    final st = await h.sync.status();
    expect(st.lastError, WorkingRoleRevokedException.workingRoleRevokedCode);
    expect((await h.evidence.byId('f1'))!.status, 'pending');
    expect((await h.outbox.counts()).pending, 1);
    expect((await h.actions.byId(a.id))!.status, ApiActionStatus.pending);

    h.fake.workingRoleRevoked = false;
    expect(await h.sync.syncOnce(force: true), isNotNull);
    expect((await h.evidence.byId('f1'))!.status, 'uploaded');
    expect((await h.outbox.counts()).pending, 0);
    expect((await h.actions.byId(a.id))!.status, ApiActionStatus.done);
  });
}
