"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { Loading } from "@/components/ui";
import { SeasonCalendars } from "@/htx/SeasonCalendars";

function Inner() {
  const params = useSearchParams();
  const ref = params.get("calendar") ?? "";
  return <SeasonCalendars key={ref} initialRef={ref} />;
}

export default function SeasonCalendarsPage() { return <Suspense fallback={<Loading />}><Inner /></Suspense>; }
