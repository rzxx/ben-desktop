import * as JobsFacade from "../../../bindings/ben/desktop/jobsfacade";

export function listJobs(libraryId = "") {
  return JobsFacade.ListJobs(libraryId);
}

export async function getJob(jobId: string) {
  const [job, found] = await JobsFacade.GetJob(jobId);
  return { found, job };
}

export function subscribeJobEvents() {
  return JobsFacade.SubscribeJobEvents();
}
