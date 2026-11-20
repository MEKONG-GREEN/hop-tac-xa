"use client";

import { useParams } from "next/navigation";
import { HTXProfileEditor } from "@/htx/HTXProfileEditor";

export default function EditHTXProfilePage() {
  const { id } = useParams<{ id: string }>();
  return <HTXProfileEditor cooperativeRef={id} />;
}
