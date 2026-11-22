import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problem";
import { ProblemAlert } from "@/components/ProblemAlert";
import { NO_COOP_NOTICE, OUT_OF_SCOPE_NOTICE, SHARING_STOPPED_NOTICE, ShellExitContext, ShellNoCoopContext, useExitOnNotFound, useExitOnSharingStopped } from "./notice";

const problemOf = (status: number, code: string) => new ApiProblem({ type: "about:blank", title: code, status, code });

function Probe({ problem }: { problem: ApiProblem | null }) {
  const leaving = useExitOnNotFound(problem);
  return <p>{leaving ? "đang rời" : "ở lại"}</p>;
}

describe("useExitOnNotFound (FN-06/EX-01, AC-N2)", () => {
  const run = (problem: ApiProblem | null, exit: ((t: string) => void) | null) =>
    render(
      <ShellExitContext.Provider value={exit}>
        <Probe problem={problem} />
      </ShellExitContext.Provider>,
    );

  it("[AC-N2] người dùng App HTX nhận 404 ở màn chi tiết → rời về trang đầu tiên được xem kèm thông báo", () => {
    const exit = vi.fn();
    run(problemOf(404, "party.org_not_found"), exit);
    expect(exit).toHaveBeenCalledWith(OUT_OF_SCOPE_NOTICE);
    expect(screen.getByText("đang rời")).toBeTruthy();
  });

  it("lỗi khác 404 hoặc chưa có lỗi → ở lại", () => {
    const exit = vi.fn();
    run(problemOf(500, "internal_error"), exit);
    run(null, exit);
    expect(exit).not.toHaveBeenCalled();
  });

  it("không có hàm rời (vai ngoài App HTX, ngoài khung AppShell) → ở lại", () => {
    run(problemOf(404, "party.org_not_found"), null);
    expect(screen.getByText("ở lại")).toBeTruthy();
  });
});

describe("<ProblemAlert> với tài khoản chưa gắn HTX (FN-06/EX-02, AC-N1)", () => {
  it("[AC-N1] node_scope_required của tài khoản App HTX chưa gắn HTX → câu liên hệ người cấp tài khoản, không có lỗi chung", () => {
    render(
      <ShellNoCoopContext.Provider value>
        <ProblemAlert problem={problemOf(403, "node_scope_required")} />
      </ShellNoCoopContext.Provider>,
    );
    expect(screen.getByText(NO_COOP_NOTICE)).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("tài khoản chưa gắn HTX nhận lỗi khác → vẫn thấy lỗi chung", () => {
    render(
      <ShellNoCoopContext.Provider value>
        <ProblemAlert problem={problemOf(500, "internal_error")} />
      </ShellNoCoopContext.Provider>,
    );
    expect(screen.getByRole("alert")).toBeTruthy();
  });

  it("ngoài khung AppShell → thông báo lỗi chung như cũ", () => {
    render(<ProblemAlert problem={problemOf(403, "node_scope_required")} />);
    expect(screen.queryByText(NO_COOP_NOTICE)).toBeNull();
    expect(screen.getByRole("alert")).toBeTruthy();
  });
});

describe("useExitOnSharingStopped (FN-06/AC-N1, AH-274)", () => {
  function SharedProbe({ problem, shared }: { problem: ApiProblem | null; shared: boolean }) {
    return <p>{useExitOnSharingStopped(problem, shared) ? "đang rời" : "ở lại"}</p>;
  }
  const run = (problem: ApiProblem | null, shared: boolean, exit: ((t: string, to?: string) => void) | null) =>
    render(
      <ShellExitContext.Provider value={exit}>
        <SharedProbe problem={problem} shared={shared} />
      </ShellExitContext.Provider>,
    );

  it.each([404, 403])("HTX đang xem ngừng chia sẻ (%i) → về danh sách HTX kèm câu ngừng chia sẻ", (status) => {
    const exit = vi.fn();
    run(problemOf(status, "party.org_not_found"), true, exit);
    expect(exit).toHaveBeenCalledWith(SHARING_STOPPED_NOTICE, "/htx");
    expect(SHARING_STOPPED_NOTICE).toBe("HTX đã ngừng chia sẻ dữ liệu cho HTX của bạn");
  });

  it("không phải màn chia sẻ hoặc lỗi khác → ở lại", () => {
    const exit = vi.fn();
    run(problemOf(404, "party.org_not_found"), false, exit);
    run(problemOf(500, "internal_error"), true, exit);
    expect(exit).not.toHaveBeenCalled();
  });
});
