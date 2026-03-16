import { Dialogs } from "@wailsio/runtime";
import * as LibraryFacade from "../../../bindings/ben/desktop/libraryfacade";

export async function getActiveLibrary() {
  const [library, found] = await LibraryFacade.ActiveLibrary();
  return { library, found };
}

export function listLibraries() {
  return LibraryFacade.ListLibraries();
}

export function createLibrary(name: string) {
  return LibraryFacade.CreateLibrary(name);
}

export function selectLibrary(libraryId: string) {
  return LibraryFacade.SelectLibrary(libraryId);
}

export function renameLibrary(libraryId: string, name: string) {
  return LibraryFacade.RenameLibrary(libraryId, name);
}

export function leaveLibrary(libraryId: string) {
  return LibraryFacade.LeaveLibrary(libraryId);
}

export function deleteLibrary(libraryId: string) {
  return LibraryFacade.DeleteLibrary(libraryId);
}

export function listLibraryMembers() {
  return LibraryFacade.ListLibraryMembers();
}

export function updateLibraryMemberRole(deviceId: string, role: string) {
  return LibraryFacade.UpdateLibraryMemberRole(deviceId, role);
}

export function removeLibraryMember(deviceId: string) {
  return LibraryFacade.RemoveLibraryMember(deviceId);
}

export function getScanRoots() {
  return LibraryFacade.ScanRoots();
}

export function addScanRoots(roots: string[]) {
  return LibraryFacade.AddScanRoots(roots);
}

export function removeScanRoots(roots: string[]) {
  return LibraryFacade.RemoveScanRoots(roots);
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

export function startLibraryRescan() {
  return LibraryFacade.StartRescanNow();
}

export function startRootRescan(root: string) {
  return LibraryFacade.StartRescanRoot(root);
}
