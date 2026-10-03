import { ApiStatus } from "@/components/api-status";
import { ProjectsHome } from "@/components/projects-home";

// The static shell only: the list is live data, rendered in the browser.
export default function Home() {
  return (
    <>
      <ProjectsHome />
      <footer className="mx-auto w-full max-w-3xl px-6 pb-8 text-xs text-zinc-500">
        <ApiStatus />
      </footer>
    </>
  );
}
