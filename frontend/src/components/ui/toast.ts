import type { ReactNode } from "react";
import { Toast } from "@base-ui/react/toast";
import type {
  ToastManagerAddOptions,
  ToastManagerUpdateOptions,
} from "@base-ui/react/toast";

export type ToastType = "info" | "success" | "error" | "loading";

export type ToastAction = {
  label: string;
  onClick: () => void;
};

export type ToastData = Record<string, unknown>;

export type ToastOptions = {
  id?: string;
  title?: ReactNode;
  description?: ReactNode;
  type?: ToastType;
  timeout?: number;
  priority?: "low" | "high";
  action?: ToastAction;
  data?: ToastData;
  onClose?: () => void;
  onRemove?: () => void;
};

export type ToastPromiseOptions<T> = {
  loading: string | ToastOptions;
  success: string | ((value: T) => string | ToastOptions);
  error: string | ((error: unknown) => string | ToastOptions);
};

export const toastManager = Toast.createToastManager<ToastData>();

function normalize(
  input: string | ToastOptions,
  defaultType: ToastType,
): ToastOptions {
  if (typeof input === "string") {
    return { title: input, type: defaultType };
  }
  return { ...input, type: input.type ?? defaultType };
}

function toAddOptions(
  options: ToastOptions,
): ToastManagerAddOptions<ToastData> {
  return {
    id: options.id,
    title: options.title,
    description: options.description,
    type: options.type,
    timeout: options.timeout,
    priority: options.priority,
    actionProps: options.action
      ? { children: options.action.label, onClick: options.action.onClick }
      : undefined,
    data: options.data,
    onClose: options.onClose,
    onRemove: options.onRemove,
  };
}

function add(options: ToastOptions): string {
  return toastManager.add(toAddOptions(options));
}

function update(id: string, options: Partial<ToastOptions>): void {
  const updates: ToastManagerUpdateOptions<ToastData> = {};

  if (options.title !== undefined) {
    updates.title = options.title;
  }
  if (options.description !== undefined) {
    updates.description = options.description;
  }
  if (options.type !== undefined) {
    updates.type = options.type;
  }
  if (options.timeout !== undefined) {
    updates.timeout = options.timeout;
  }
  if (options.priority !== undefined) {
    updates.priority = options.priority;
  }
  if (options.action !== undefined) {
    updates.actionProps = options.action
      ? { children: options.action.label, onClick: options.action.onClick }
      : undefined;
  }
  if (options.data !== undefined) {
    updates.data = options.data;
  }
  if (options.onClose !== undefined) {
    updates.onClose = options.onClose;
  }
  if (options.onRemove !== undefined) {
    updates.onRemove = options.onRemove;
  }

  toastManager.update(id, updates);
}

function dismiss(id?: string): void {
  toastManager.close(id);
}

function toPromiseUpdateOptions(
  input: string | ToastOptions,
  defaultType: ToastType,
): ToastManagerUpdateOptions<ToastData> {
  return toAddOptions(normalize(input, defaultType));
}

function promise<T>(
  promiseValue: Promise<T>,
  options: ToastPromiseOptions<T>,
): Promise<T> {
  return toastManager.promise(promiseValue, {
    loading: toPromiseUpdateOptions(options.loading, "loading"),
    success: (value) => {
      const input =
        typeof options.success === "function"
          ? options.success(value)
          : options.success;
      return toPromiseUpdateOptions(input, "success");
    },
    error: (error) => {
      const input =
        typeof options.error === "function"
          ? options.error(error)
          : options.error;
      return toPromiseUpdateOptions(input, "error");
    },
  });
}

function info(input: string | Omit<ToastOptions, "type">): string {
  return add(normalize(input, "info"));
}

function success(input: string | Omit<ToastOptions, "type">): string {
  return add(normalize(input, "success"));
}

function error(input: string | Omit<ToastOptions, "type">): string {
  return add(normalize(input, "error"));
}

function loading(input: string | Omit<ToastOptions, "type">): string {
  return add(normalize(input, "loading"));
}

export const toast = Object.assign(
  (input: string | ToastOptions): string => add(normalize(input, "info")),
  { info, success, error, loading, promise, dismiss, update },
);
