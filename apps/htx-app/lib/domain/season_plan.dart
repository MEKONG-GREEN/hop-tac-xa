/// Lịch do máy chủ tính từ SOP snapshot; thiết bị không tự suy lịch bằng
/// day_offset/window_days cũ và không ghi thay đổi vào dữ liệu vụ.
class SeasonPlan {
  const SeasonPlan({required this.asOfDate, required this.stages});
  final String asOfDate;
  final List<SeasonPlanStage> stages;

  factory SeasonPlan.fromJson(Map<String, Object?> json) => SeasonPlan(
    asOfDate: json['as_of_date'] as String,
    stages: _objects(json['stages']).map(SeasonPlanStage.fromJson).toList(),
  );
}

class SeasonPlanStage {
  const SeasonPlanStage({
    required this.seq,
    required this.name,
    required this.steps,
  });
  final int seq;
  final String name;
  final List<SeasonPlanStep> steps;

  factory SeasonPlanStage.fromJson(Map<String, Object?> json) =>
      SeasonPlanStage(
        seq: json['seq'] as int,
        name: json['name'] as String,
        steps: _objects(json['steps']).map(SeasonPlanStep.fromJson).toList(),
      );
}

class SeasonPlanStep {
  const SeasonPlanStep({
    required this.seq,
    required this.name,
    required this.status,
    required this.undated,
    this.fromDate,
    this.toDate,
  });
  final int seq;
  final String name;
  final String status;
  final bool undated;
  final String? fromDate;
  final String? toDate;

  factory SeasonPlanStep.fromJson(Map<String, Object?> json) {
    final status = json['status'] as String;
    if (!{'upcoming', 'due', 'overdue', 'waiting_anchor'}.contains(status)) {
      throw const FormatException('Trạng thái kế hoạch không hợp lệ');
    }
    return SeasonPlanStep(
      seq: json['seq'] as int,
      name: json['name'] as String,
      status: status,
      undated: json['undated'] as bool,
      fromDate: json['from_date'] as String?,
      toDate: json['to_date'] as String?,
    );
  }
}

Iterable<Map<String, Object?>> _objects(Object? value) =>
    (value as List<Object?>).map(
      (item) => Map<String, Object?>.from(item as Map),
    );
