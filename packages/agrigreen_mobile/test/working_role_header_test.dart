import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test(
    'each request uses the current working role and the original bearer',
    () async {
      String? role = 'htx_member';
      final seen = <http.Request>[];
      final api = HttpSyncApi(
        baseUrl: 'https://api.test',
        accessToken: () async => 'original-jwt',
        workingRole: () => role,
        client: MockClient((request) async {
          seen.add(request);
          return http.Response(
            '{}',
            200,
            headers: {'Content-Type': 'application/json'},
          );
        }),
      );
      await api.getJson('/v1/me');
      role = 'coop_manager';
      await api.getJson('/v1/me');
      role = null;
      await api.getJson('/v1/me');
      expect(seen.map((request) => request.headers['X-HTX-Role']), [
        'htx_member',
        'coop_manager',
        null,
      ]);
      expect(
        seen.every(
          (request) =>
              request.headers['Authorization'] == 'Bearer original-jwt',
        ),
        isTrue,
      );
    },
  );
}
