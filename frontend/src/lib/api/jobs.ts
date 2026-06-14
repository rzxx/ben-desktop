import * as JobsFacade from "../../../bindings/ben/desktop/jobsfacade";
import { traceWailsCall } from "@/lib/observability/trace";

export function listJobs(libraryId = "") {
  return traceWailsCall("jobs", "list_jobs", { libraryId }, () =>
    JobsFacade.ListJobs(libraryId),
  );
}

export async function getJob(jobId: string) {
  const [job, found] = await traceWailsCall("jobs", "get_job", { jobId }, () =>
    JobsFacade.GetJob(jobId),
  );
  return { found, job };
}

export function subscribeJobEvents() {
  return JobsFacade.SubscribeJobEvents();
}
