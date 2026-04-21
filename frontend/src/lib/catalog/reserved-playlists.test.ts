import { describe, expect, test } from "vitest";
import { Types } from "@/lib/api/models";
import {
  playlistEntityKey,
  reservedPlaylistAlias,
  reservedPlaylistRoute,
} from "./reserved-playlists";

describe("reserved playlist helpers", () => {
  test("maps reserved playlist kinds to stable aliases", () => {
    expect(reservedPlaylistAlias("liked")).toBe("liked");
    expect(reservedPlaylistAlias("offline")).toBe("offline");
    expect(reservedPlaylistAlias("normal")).toBeNull();
  });

  test("uses reserved aliases for playlist entity keys", () => {
    expect(
      playlistEntityKey({
        Kind: Types.PlaylistKind.PlaylistKindLiked,
        PlaylistID: "liked-synthetic",
      }),
    ).toBe("liked");
    expect(
      playlistEntityKey({
        Kind: Types.PlaylistKind.PlaylistKindOffline,
        PlaylistID: "offline-synthetic",
      }),
    ).toBe("offline");
    expect(
      playlistEntityKey({
        Kind: Types.PlaylistKind.PlaylistKindNormal,
        PlaylistID: "playlist-1",
      }),
    ).toBe("playlist-1");
  });

  test("routes reserved playlists to dedicated pages", () => {
    expect(reservedPlaylistRoute("liked")).toBe("/playlists/liked");
    expect(reservedPlaylistRoute("offline")).toBe("/playlists/offline");
    expect(reservedPlaylistRoute("normal")).toBeNull();
  });
});
