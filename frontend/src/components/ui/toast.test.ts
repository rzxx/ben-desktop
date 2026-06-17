import { describe, expect, test } from "vitest";
import { toast } from "@/components/ui/toast";

describe("toast API", () => {
  test("adds an info toast from a string", () => {
    const id = toast("Hello");
    expect(typeof id).toBe("string");
    expect(id).not.toBe("");
    toast.dismiss(id);
  });

  test("adds typed toasts", () => {
    const successId = toast.success("Saved");
    const errorId = toast.error("Failed");
    const loadingId = toast.loading("Loading");

    expect(typeof successId).toBe("string");
    expect(typeof errorId).toBe("string");
    expect(typeof loadingId).toBe("string");

    toast.dismiss(successId);
    toast.dismiss(errorId);
    toast.dismiss(loadingId);
  });

  test("updates an existing toast", () => {
    const id = toast.info({ title: "Loading" });
    toast.update(id, { title: "Still loading", type: "loading" });
    toast.dismiss(id);
  });

  test("dismisses all toasts when called without id", () => {
    toast.success("One");
    toast.success("Two");
    expect(() => toast.dismiss()).not.toThrow();
  });

  test("promise resolves to a success toast", async () => {
    const result = await toast.promise(Promise.resolve(42), {
      loading: "Working",
      success: "Done",
      error: "Failed",
    });
    expect(result).toBe(42);
  });

  test("promise rejects and surfaces an error toast", async () => {
    await expect(
      toast.promise(Promise.reject(new Error("nope")), {
        loading: "Working",
        success: "Done",
        error: (err) => (err instanceof Error ? err.message : "Failed"),
      }),
    ).rejects.toThrow("nope");
  });

  test("supports an action", () => {
    const onClick = () => {};
    const id = toast.info({
      title: "Undo?",
      action: { label: "Undo", onClick },
    });
    toast.dismiss(id);
  });
});
