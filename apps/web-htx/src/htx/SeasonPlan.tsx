import { formatDate } from "@/lib/format";

/** Read view only; the API remains responsible for dates and permissions. */
export interface SeasonPlanView {
  as_of_date: string;
  stages: {
    seq: number;
    name: string;
    steps: {
      seq: number;
      name: string;
      from_date: string | null;
      to_date: string | null;
      undated: boolean;
      status: "upcoming" | "due" | "overdue" | "waiting_anchor";
    }[];
  }[];
}

const STATUS = {
  upcoming: ["Chưa đến hạn", "c-mute"],
  due: ["Đến hạn", "c-ok"],
  overdue: ["Trễ hạn", "c-bad"],
  waiting_anchor: ["Chờ mốc", "c-warn"],
} as const;

export function SeasonPlan({ plan }: { plan: SeasonPlanView }) {
  return <section aria-label="Kế hoạch dự kiến">
    <h3>Kế hoạch dự kiến</h3>
    <p className="muted">Tính đến ngày {formatDate(plan.as_of_date)}</p>
    {plan.stages.length === 0 && <p>Chưa có việc trong kế hoạch.</p>}
    {plan.stages.map((stage) => <section key={stage.seq} aria-label={stage.name}>
      <h4>{stage.seq}. {stage.name}</h4>
      <div className="tbl-wrap"><table className="tbl">
        <thead><tr><th>Việc</th><th>Ngày dự kiến</th><th>Trạng thái</th></tr></thead>
        <tbody>{stage.steps.map((step) => <tr key={step.seq}>
          <td>{step.name}</td>
          <td>{step.undated ? "Chưa đặt ngày" : !step.from_date ? "Chưa có mốc để tính ngày"
            : step.to_date ? `${formatDate(step.from_date)} – ${formatDate(step.to_date)}`
              : `Từ ${formatDate(step.from_date)}`}</td>
          <td><span className={`chip ${STATUS[step.status][1]}`}>{STATUS[step.status][0]}</span></td>
        </tr>)}</tbody>
      </table></div>
    </section>)}
  </section>;
}
