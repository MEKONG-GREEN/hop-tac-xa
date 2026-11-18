import 'package:flutter/material.dart';

import '../domain/season_plan.dart';

String _day(String value) => value.split('-').reversed.join('/');

/// Cùng màn đọc cho mọi vai được xem kế hoạch. Quyền tính lại được kiểm riêng
/// ở API và luồng mở vụ, không có thao tác ghi trong widget này.
class SeasonPlanView extends StatelessWidget {
  const SeasonPlanView({super.key, required this.plan});
  final SeasonPlan plan;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text('Kế hoạch dự kiến', style: Theme.of(context).textTheme.titleMedium),
      Text('Tính đến ngày ${_day(plan.asOfDate)}'),
      if (plan.stages.isEmpty) const Text('Chưa có việc trong kế hoạch.'),
      for (final stage in plan.stages)
        Card(
          child: ExpansionTile(
            key: ValueKey('plan-stage-${stage.seq}'),
            initiallyExpanded: true,
            title: Text('${stage.seq}. ${stage.name}'),
            children: [
              for (final step in stage.steps)
                ListTile(
                  title: Text(step.name),
                  subtitle: Text(
                    step.undated
                        ? 'Chưa đặt ngày'
                        : step.fromDate == null
                        ? 'Chưa có mốc để tính ngày'
                        : step.toDate == null
                        ? 'Từ ${_day(step.fromDate!)}'
                        : '${_day(step.fromDate!)} – ${_day(step.toDate!)}',
                  ),
                  trailing: Text(switch (step.status) {
                    'upcoming' => 'Chưa đến hạn',
                    'due' => 'Đến hạn',
                    'overdue' => 'Trễ hạn',
                    _ => 'Chờ mốc',
                  }),
                ),
            ],
          ),
        ),
    ],
  );
}
