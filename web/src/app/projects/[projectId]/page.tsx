import { Suspense } from "react";

import { ProjectView } from "@/components/project-view";

// The static shell only. Project data is live, so it's loaded and kept in
// sync in the browser; anything rendered here would be stale a moment later.
export default async function ProjectPage({ params }: PageProps<"/projects/[projectId]">) {
  const { projectId } = await params;
  return (
    // ProjectView reads ?task=, which needs a Suspense boundary.
    <Suspense fallback={<p className="p-6 text-sm text-zinc-500">Loading project…</p>}>
      <ProjectView key={projectId} projectId={projectId} />
    </Suspense>
  );
}
