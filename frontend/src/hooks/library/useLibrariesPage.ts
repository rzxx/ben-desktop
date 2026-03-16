import { useEffect, useState } from "react";
import type {
  LibraryMemberStatus,
  LibrarySummary,
  LocalContext,
} from "@/lib/api/models";
import {
  createLibrary,
  deleteLibrary,
  getActiveLibrary,
  leaveLibrary,
  listLibraries,
  listLibraryMembers,
  removeLibraryMember,
  renameLibrary,
  selectLibrary,
  updateLibraryMemberRole,
} from "@/lib/api/library";
import { getLocalContext } from "@/lib/api/network";

type LibrariesState = {
  active: LibrarySummary | null;
  error: string;
  libraries: LibrarySummary[];
  loading: boolean;
  local: LocalContext | null;
  members: LibraryMemberStatus[];
};

const initialState: LibrariesState = {
  active: null,
  error: "",
  libraries: [],
  loading: true,
  local: null,
  members: [],
};

function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function normalizeRole(role: string) {
  return role.trim().toLowerCase();
}

export function canManageLibrary(role: string) {
  const normalized = normalizeRole(role);
  return normalized === "owner" || normalized === "admin";
}

export function normalizeLibraryRole(role: string) {
  return normalizeRole(role);
}

export function useLibrariesPage() {
  const [state, setState] = useState<LibrariesState>(initialState);
  const [createName, setCreateName] = useState("");
  const [renameName, setRenameName] = useState("");
  const [pendingAction, setPendingAction] = useState("");
  const [actionError, setActionError] = useState("");
  const [notice, setNotice] = useState("");

  async function refresh() {
    try {
      const [libraries, activeResult, local] = await Promise.all([
        listLibraries(),
        getActiveLibrary(),
        getLocalContext(),
      ]);
      const active = activeResult.found ? activeResult.library : null;
      const members = active ? await listLibraryMembers() : [];
      setState({
        active,
        error: "",
        libraries,
        loading: false,
        local,
        members,
      });
      setRenameName(active?.Name ?? "");
    } catch (error) {
      setState((current) => ({
        ...current,
        error: describeError(error),
        loading: false,
      }));
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function runAction(
    actionKey: string,
    work: () => Promise<unknown>,
    successMessage: string,
  ) {
    setPendingAction(actionKey);
    setActionError("");
    setNotice("");
    try {
      await work();
      setNotice(successMessage);
      await refresh();
    } catch (error) {
      setActionError(describeError(error));
    } finally {
      setPendingAction("");
    }
  }

  const activeRole = state.active?.Role ?? state.local?.Role ?? "";

  return {
    actionError,
    canManage: canManageLibrary(activeRole),
    createName,
    notice,
    pendingAction,
    refresh,
    renameName,
    runAction,
    setCreateName,
    setRenameName,
    state,
    actions: {
      createLibrary,
      deleteLibrary,
      leaveLibrary,
      removeLibraryMember,
      renameLibrary,
      selectLibrary,
      updateLibraryMemberRole,
    },
  };
}
