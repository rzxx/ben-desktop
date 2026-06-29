import { Dialogs } from "@wailsio/runtime";
import * as LibraryFacade from "../../../bindings/ben/desktop/libraryfacade";
import { Types } from "./models";
import { traceWailsCall } from "@/lib/observability/trace";

export async function getActiveLibrary() {
  const [library, found] = await traceWailsCall(
    "library",
    "active_library",
    undefined,
    () => LibraryFacade.ActiveLibrary(),
  );
  return { library, found };
}

export function listLibraries() {
  return traceWailsCall("library", "list_libraries", undefined, () =>
    LibraryFacade.ListLibraries(),
  );
}

export function createLibrary(name: string) {
  return traceWailsCall("library", "create_library", { name }, () =>
    LibraryFacade.CreateLibrary(name),
  );
}

export function selectLibrary(libraryId: string) {
  return traceWailsCall("library", "select_library", { libraryId }, () =>
    LibraryFacade.SelectLibrary(libraryId),
  );
}

export function renameLibrary(libraryId: string, name: string) {
  return traceWailsCall("library", "rename_library", { libraryId, name }, () =>
    LibraryFacade.RenameLibrary(libraryId, name),
  );
}

export function leaveLibrary(libraryId: string) {
  return traceWailsCall("library", "leave_library", { libraryId }, () =>
    LibraryFacade.LeaveLibrary(libraryId),
  );
}

export function deleteLibrary(libraryId: string) {
  return traceWailsCall("library", "delete_library", { libraryId }, () =>
    LibraryFacade.DeleteLibrary(libraryId),
  );
}

export function listLibraryMembers() {
  return traceWailsCall("library", "list_library_members", undefined, () =>
    LibraryFacade.ListLibraryMembers(),
  );
}

export function updateLibraryMemberRole(deviceId: string, role: string) {
  return traceWailsCall(
    "library",
    "update_library_member_role",
    { deviceId, role },
    () => LibraryFacade.UpdateLibraryMemberRole(deviceId, role),
  );
}

export function removeLibraryMember(deviceId: string) {
  return traceWailsCall("library", "remove_library_member", { deviceId }, () =>
    LibraryFacade.RemoveLibraryMember(deviceId),
  );
}

export function getLibraryRelayConfig(libraryId = "") {
  return traceWailsCall(
    "library",
    "get_library_relay_config",
    { libraryId },
    () => LibraryFacade.GetLibraryRelayConfig(libraryId),
  );
}

export function updateLibraryRelayConfig(
  req: InstanceType<typeof Types.UpdateLibraryRelayConfigRequest>,
) {
  return traceWailsCall(
    "library",
    "update_library_relay_config",
    {
      libraryId: req.LibraryID,
      registryURL: req.RegistryURL,
      relayBootstrapCount: req.RelayBootstrapAddrs.length,
    },
    () => LibraryFacade.UpdateLibraryRelayConfig(req),
  );
}

export function getScanRoots() {
  return traceWailsCall("library", "scan_roots", undefined, () =>
    LibraryFacade.ScanRoots(),
  );
}

export function addScanRoots(roots: string[]) {
  return traceWailsCall(
    "library",
    "add_scan_roots",
    { count: roots.length },
    () => LibraryFacade.AddScanRoots(roots),
  );
}

export function removeScanRoots(roots: string[]) {
  return traceWailsCall(
    "library",
    "remove_scan_roots",
    { count: roots.length },
    () => LibraryFacade.RemoveScanRoots(roots),
  );
}

export async function pickScanRoot(currentRoot = "") {
  const selected = await Dialogs.OpenFile({
    AllowsMultipleSelection: false,
    ButtonText: "Add root",
    CanChooseDirectories: true,
    CanChooseFiles: false,
    CanCreateDirectories: false,
    Directory: currentRoot || undefined,
    Message: "Choose a directory to scan on this device.",
    Title: "Add scan root",
  });

  return typeof selected === "string" ? selected.trim() : "";
}

export function startLibraryRepair() {
  return traceWailsCall("library", "start_repair_library", undefined, () =>
    LibraryFacade.StartRepairLibrary(),
  );
}
