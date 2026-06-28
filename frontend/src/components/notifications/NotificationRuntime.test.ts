import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { Types } from "@/lib/api/models";

const { addToastMock, dismissToastMock, updateToastMock } = vi.hoisted(() => ({
  addToastMock: vi.fn((options: { id?: string }) => options.id ?? "toast"),
  dismissToastMock: vi.fn(),
  updateToastMock: vi.fn(),
}));

vi.mock("@/components/ui/toast", () => ({
  toast: Object.assign(addToastMock, {
    dismiss: dismissToastMock,
    update: updateToastMock,
  }),
}));

import { NotificationRuntime } from "./NotificationRuntime";
import { useNotificationsStore } from "@/stores/notifications/store";

function makeNotification(source: Partial<Types.NotificationSnapshot> = {}) {
  return new Types.NotificationSnapshot({
    id: "notification-1",
    kind: "repair-library",
    audience: Types.NotificationAudience.NotificationAudienceUser,
    importance: Types.NotificationImportance.NotificationImportanceNormal,
    phase: Types.NotificationPhase.NotificationPhaseSuccess,
    message: "Done",
    createdAt: "2026-03-21T10:00:00.000Z",
    updatedAt: "2026-03-21T10:00:00.000Z",
    ...source,
  });
}

describe("NotificationRuntime", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (
      globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
    ).IS_REACT_ACT_ENVIRONMENT = true;
    sessionStorage.clear();
    addToastMock.mockClear();
    dismissToastMock.mockClear();
    updateToastMock.mockClear();
    useNotificationsStore.setState({
      notifications: [],
      preferences: new Types.NotificationPreferences({
        verbosity:
          Types.NotificationVerbosity.NotificationVerbosityUserActivity,
      }),
    });
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  test("does not reset an unchanged toast when another notification arrives", async () => {
    const completed = makeNotification();
    useNotificationsStore.setState({ notifications: [completed] });

    await act(async () => root.render(createElement(NotificationRuntime)));

    expect(addToastMock).toHaveBeenCalledTimes(1);

    const running = makeNotification({
      id: "notification-2",
      phase: Types.NotificationPhase.NotificationPhaseRunning,
      message: "Working",
      updatedAt: "2026-03-21T10:00:01.000Z",
    });
    await act(async () =>
      useNotificationsStore.setState({ notifications: [running, completed] }),
    );

    expect(addToastMock).toHaveBeenCalledTimes(2);
    expect(updateToastMock).not.toHaveBeenCalled();

    const updated = makeNotification({
      message: "Done with details",
      updatedAt: "2026-03-21T10:00:02.000Z",
    });
    await act(async () =>
      useNotificationsStore.setState({ notifications: [updated, running] }),
    );

    expect(updateToastMock).toHaveBeenCalledTimes(1);
    expect(updateToastMock).toHaveBeenCalledWith(
      "notification-1",
      expect.objectContaining({ description: "Done with details" }),
    );
  });
});
