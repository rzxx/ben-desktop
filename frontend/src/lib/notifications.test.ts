import { describe, expect, test } from "vitest";
import { Types } from "./api/models";
import {
  applyNotificationSnapshotBatch,
  hasNotificationToastBeenShown,
  matchesNotificationFilter,
  notificationHeading,
  readNotificationToastHistory,
  recordNotificationToastShown,
  serializeNotificationToastHistory,
  shouldToastNotification,
  upsertNotificationSnapshots,
} from "./notifications";

function makeNotification(source: Partial<Types.NotificationSnapshot> = {}) {
  return new Types.NotificationSnapshot({
    id: "notification-1",
    kind: "repair-library",
    audience: Types.NotificationAudience.NotificationAudienceUser,
    importance: Types.NotificationImportance.NotificationImportanceNormal,
    phase: Types.NotificationPhase.NotificationPhaseRunning,
    createdAt: "2026-03-21T10:00:00.000Z",
    updatedAt: "2026-03-21T10:00:00.000Z",
    ...source,
  });
}

describe("upsertNotificationSnapshots", () => {
  test("replaces an existing notification by stable id", () => {
    const current = [
      makeNotification({
        id: "scan-1",
        message: "Queued",
        updatedAt: "2026-03-21T10:00:00.000Z",
      }),
      makeNotification({
        id: "sync-1",
        kind: "sync-now",
        updatedAt: "2026-03-21T09:59:00.000Z",
      }),
    ];

    const next = makeNotification({
      id: "scan-1",
      message: "Running",
      phase: Types.NotificationPhase.NotificationPhaseRunning,
      updatedAt: "2026-03-21T10:01:00.000Z",
    });

    const updated = upsertNotificationSnapshots(current, next);

    expect(updated).toHaveLength(2);
    expect(updated[0].id).toBe("scan-1");
    expect(updated[0].message).toBe("Running");
    expect(updated[1].id).toBe("sync-1");
  });

  test("keeps the same array reference when nothing changed", () => {
    const current = [
      makeNotification({
        id: "scan-1",
        message: "Running",
        updatedAt: "2026-03-21T10:01:00.000Z",
      }),
    ];

    const updated = upsertNotificationSnapshots(current, current[0]);

    expect(updated).toBe(current);
  });
});

describe("applyNotificationSnapshotBatch", () => {
  test("applies the latest snapshot per id and sorts once at the end", () => {
    const current = [
      makeNotification({
        id: "sync-1",
        kind: "sync-now",
        updatedAt: "2026-03-21T10:00:00.000Z",
      }),
      makeNotification({
        id: "scan-1",
        updatedAt: "2026-03-21T09:59:00.000Z",
      }),
    ];

    const updated = applyNotificationSnapshotBatch(current, [
      makeNotification({
        id: "scan-1",
        message: "Running slowly",
        updatedAt: "2026-03-21T10:01:00.000Z",
      }),
      makeNotification({
        id: "scan-1",
        message: "Running fast",
        updatedAt: "2026-03-21T10:02:00.000Z",
      }),
    ]);

    expect(updated).toHaveLength(2);
    expect(updated[0].id).toBe("scan-1");
    expect(updated[0].message).toBe("Running fast");
    expect(updated[1].id).toBe("sync-1");
  });
});

describe("shouldToastNotification", () => {
  test("always surfaces failures regardless of verbosity", () => {
    const failure = makeNotification({
      audience: Types.NotificationAudience.NotificationAudienceSystem,
      importance: Types.NotificationImportance.NotificationImportanceDebug,
      phase: Types.NotificationPhase.NotificationPhaseError,
    });

    expect(shouldToastNotification(failure, "important")).toBe(true);
    expect(shouldToastNotification(failure, "user_activity")).toBe(true);
  });

  test("filters user and system work by verbosity tier", () => {
    const importantUser = makeNotification({
      audience: Types.NotificationAudience.NotificationAudienceUser,
      importance: Types.NotificationImportance.NotificationImportanceImportant,
    });
    const normalUser = makeNotification();
    const systemDebug = makeNotification({
      audience: Types.NotificationAudience.NotificationAudienceSystem,
      importance: Types.NotificationImportance.NotificationImportanceDebug,
    });

    expect(shouldToastNotification(importantUser, "important")).toBe(true);
    expect(shouldToastNotification(normalUser, "important")).toBe(false);
    expect(shouldToastNotification(normalUser, "user_activity")).toBe(true);
    expect(shouldToastNotification(systemDebug, "user_activity")).toBe(false);
    expect(shouldToastNotification(systemDebug, "everything")).toBe(true);
  });

  test("suppresses playback success toasts while keeping failures toastable", () => {
    const playbackSuccess = makeNotification({
      kind: "playback-loading",
      importance: Types.NotificationImportance.NotificationImportanceImportant,
      phase: Types.NotificationPhase.NotificationPhaseSuccess,
    });
    const preloadSuccess = makeNotification({
      kind: "playback-preload",
      audience: Types.NotificationAudience.NotificationAudienceSystem,
      importance: Types.NotificationImportance.NotificationImportanceNormal,
      phase: Types.NotificationPhase.NotificationPhaseSuccess,
    });
    const preloadFailure = makeNotification({
      kind: "playback-preload",
      audience: Types.NotificationAudience.NotificationAudienceSystem,
      importance: Types.NotificationImportance.NotificationImportanceNormal,
      phase: Types.NotificationPhase.NotificationPhaseError,
    });

    expect(shouldToastNotification(playbackSuccess, "user_activity")).toBe(
      false,
    );
    expect(shouldToastNotification(preloadSuccess, "everything")).toBe(false);
    expect(shouldToastNotification(preloadFailure, "important")).toBe(true);
  });
});

describe("notification toast history", () => {
  test("tracks the latest shown snapshot per notification id", () => {
    const queued = makeNotification({
      id: "scan-1",
      updatedAt: "2026-03-21T10:00:00.000Z",
    });
    const completed = makeNotification({
      id: "scan-1",
      phase: Types.NotificationPhase.NotificationPhaseSuccess,
      updatedAt: "2026-03-21T10:01:00.000Z",
    });

    const afterQueued = recordNotificationToastShown({}, queued);
    const afterCompleted = recordNotificationToastShown(afterQueued, completed);

    expect(hasNotificationToastBeenShown(queued, afterQueued)).toBe(true);
    expect(hasNotificationToastBeenShown(completed, afterQueued)).toBe(false);
    expect(hasNotificationToastBeenShown(completed, afterCompleted)).toBe(true);
  });

  test("ignores malformed persisted history", () => {
    expect(readNotificationToastHistory("not-json")).toEqual({});
    expect(
      readNotificationToastHistory(
        '{"scan-1":"bad","scan-2":0,"scan-3":1700000000000}',
      ),
    ).toEqual({
      "scan-3": 1700000000000,
    });
  });

  test("serializes a stable persisted history payload", () => {
    const history = {
      "scan-1": 1700000000000,
      "scan-2": 1700000001000,
    };

    expect(
      readNotificationToastHistory(serializeNotificationToastHistory(history)),
    ).toEqual(history);
  });
});

describe("matchesNotificationFilter", () => {
  test("keeps user and system filters separate", () => {
    const userNotification = makeNotification({
      audience: Types.NotificationAudience.NotificationAudienceUser,
    });
    const systemNotification = makeNotification({
      audience: Types.NotificationAudience.NotificationAudienceSystem,
    });

    expect(matchesNotificationFilter(userNotification, "all")).toBe(true);
    expect(matchesNotificationFilter(userNotification, "user")).toBe(true);
    expect(matchesNotificationFilter(userNotification, "system")).toBe(false);
    expect(matchesNotificationFilter(systemNotification, "system")).toBe(true);
  });
});

describe("notificationHeading", () => {
  test("uses a stable label for playback skip notifications without a subject", () => {
    expect(
      notificationHeading(
        makeNotification({
          kind: "playback-skip",
          subject: null,
        }),
      ),
    ).toBe("Playback queue");
  });

  test("labels preload and background sync notifications", () => {
    expect(
      notificationHeading(makeNotification({ kind: "playback-preload" })),
    ).toBe("Playback preload");
    expect(
      notificationHeading(makeNotification({ kind: "sync-activity" })),
    ).toBe("Background sync");
    expect(
      notificationHeading(
        makeNotification({ kind: "refresh-pinned-recording" }),
      ),
    ).toBe("Pinned track refresh");
    expect(
      notificationHeading(makeNotification({ kind: "scan-maintenance" })),
    ).toBe("Library maintenance");
  });
});
