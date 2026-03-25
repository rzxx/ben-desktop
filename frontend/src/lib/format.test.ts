import { describe, expect, test } from "vitest";
import {
  isAggregateAvailabilityPlayable,
  isTrackCollectionPlayable,
} from "./format";

describe("isAggregateAvailabilityPlayable", () => {
  test("keeps play enabled while availability has not loaded yet", () => {
    expect(isAggregateAvailabilityPlayable(undefined)).toBe(true);
  });

  test("returns false when aggregate availability reports nothing playable now", () => {
    expect(
      isAggregateAvailabilityPlayable({
        State: "OFFLINE",
        AvailableNowTrackCount: 0,
        TrackCount: 8,
      }),
    ).toBe(false);
  });

  test("returns true when at least one track is playable now", () => {
    expect(
      isAggregateAvailabilityPlayable({
        State: "PARTIAL",
        AvailableNowTrackCount: 1,
        TrackCount: 8,
      }),
    ).toBe(true);
  });
});

describe("isTrackCollectionPlayable", () => {
  test("returns false for empty collections", () => {
    expect(isTrackCollectionPlayable({ trackCount: 0 })).toBe(false);
  });

  test("keeps play enabled for non-empty collections until all tracks are known", () => {
    expect(
      isTrackCollectionPlayable({
        trackCount: 5,
        fullyLoaded: false,
        hasPlayableLoadedTrack: false,
      }),
    ).toBe(true);
  });

  test("returns false when the full loaded collection has no playable tracks", () => {
    expect(
      isTrackCollectionPlayable({
        trackCount: 5,
        fullyLoaded: true,
        hasPlayableLoadedTrack: false,
      }),
    ).toBe(false);
  });
});
