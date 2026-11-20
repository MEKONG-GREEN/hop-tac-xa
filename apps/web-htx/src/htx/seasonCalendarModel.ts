import type { components } from "@/api/generated/application";

export type SeasonCalendar = components["schemas"]["SeasonCalendar"];
export type CalendarDraft = components["schemas"]["SeasonCalendarInput"];
export type CalendarPrefill = Pick<SeasonCalendar, "crop_code" | "season_code" | "season_year" | "sow_from">;

export const calendarRefPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function calendarDraftError(draft: CalendarDraft): string | null {
  if (!draft.crop_code || !draft.season_code) return "Chọn loại cây và mùa vụ.";
  if (!Number.isInteger(draft.season_year) || draft.season_year < 2000 || draft.season_year > 2100) return "Năm từ 2000 đến 2100.";
  if (!draft.sow_from || !draft.sow_to || draft.sow_from > draft.sow_to) return "Ngày từ không được sau ngày đến.";
  if (!draft.recipients.length || draft.recipients.length > 500) return "Chọn từ 1 đến 500 Thành viên HTX.";
  return null;
}
