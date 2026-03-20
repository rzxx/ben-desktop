import { redirect } from "@tanstack/react-router";
import { getActiveLibrary } from "@/lib/api/library";

function errorMessage(error: unknown) {
  if (error instanceof Error) {
    return error.message;
  }
  if (
    error &&
    typeof error === "object" &&
    "message" in error &&
    typeof error.message === "string"
  ) {
    return error.message;
  }
  return String(error);
}

function isNoActiveLibraryError(error: unknown) {
  return (
    errorMessage(error).trim().toLowerCase() === "no active library selected"
  );
}

export async function redirectToStartupRoute() {
  const { library, found } = await getActiveLibrary();
  throw redirect({
    replace: true,
    to: found && library.LibraryID ? "/albums" : "/libraries",
  });
}

export async function withActiveLibraryRoute(work: () => Promise<unknown>) {
  try {
    await work();
  } catch (error) {
    if (isNoActiveLibraryError(error)) {
      throw redirect({ replace: true, to: "/libraries" });
    }
    throw error;
  }
}
