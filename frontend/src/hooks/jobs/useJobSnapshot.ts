import { Events } from "@wailsio/runtime";
import { useEffect, useState } from "react";
import { getJob, subscribeJobEvents } from "@/lib/api/jobs";
import { DesktopCoreModels, type JobSnapshot } from "@/lib/api/models";

export function isJobActive(job?: JobSnapshot | null) {
  return (
    job?.phase === DesktopCoreModels.JobPhase.JobPhaseQueued ||
    job?.phase === DesktopCoreModels.JobPhase.JobPhaseRunning
  );
}

export function isJobFailed(job?: JobSnapshot | null) {
  return job?.phase === DesktopCoreModels.JobPhase.JobPhaseFailed;
}

function jobTimestamp(value?: Date | string | null) {
  if (!value) {
    return 0;
  }
  const timestamp = new Date(value).getTime();
  return Number.isFinite(timestamp) ? timestamp : 0;
}

function pickLatestJobSnapshot(
  trackedJob: JobSnapshot | null,
  subscribedJob: JobSnapshot | null,
) {
  if (!trackedJob) {
    return subscribedJob;
  }
  if (!subscribedJob) {
    return trackedJob;
  }
  if (trackedJob.jobId !== subscribedJob.jobId) {
    return trackedJob;
  }
  return jobTimestamp(subscribedJob.updatedAt) >=
    jobTimestamp(trackedJob.updatedAt)
    ? subscribedJob
    : trackedJob;
}

export function useJobSnapshot(trackedJob: JobSnapshot | null) {
  const [snapshot, setSnapshot] = useState<JobSnapshot | null>(trackedJob);
  const jobId = trackedJob?.jobId?.trim() ?? "";

  useEffect(() => {
    if (!jobId) {
      return;
    }

    let disposed = false;
    let stopListening: (() => void) | undefined;

    void getJob(jobId)
      .then(({ found, job }) => {
        if (!disposed && found) {
          setSnapshot(job);
        }
      })
      .catch(() => {});

    void subscribeJobEvents()
      .then((eventName) => {
        if (disposed) {
          return;
        }
        stopListening = Events.On(eventName, (event) => {
          const next = DesktopCoreModels.JobSnapshot.createFrom(event.data);
          if (next.jobId !== jobId) {
            return;
          }
          setSnapshot(next);
        });
      })
      .catch(() => {});

    return () => {
      disposed = true;
      stopListening?.();
    };
  }, [jobId]);

  return pickLatestJobSnapshot(trackedJob, snapshot);
}
