import { Events } from "@wailsio/runtime";
import { useEffect, useState } from "react";
import {
  listPinStates,
  PIN_PROFILE,
  subscribePinEvents,
} from "@/lib/api/pin";
import { Types, type PinState, type PinSubjectRef } from "@/lib/api/models";

function normalizeSubjects(subjects: PinSubjectRef[]) {
  const out: PinSubjectRef[] = [];
  const seen = new Set<string>();

  for (const subject of subjects) {
    const kind = subject.Kind;
    const id = subject.ID?.trim() ?? "";
    if (!kind || !id) {
      continue;
    }
    const key = `${kind}:${id}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push(
      new Types.PinSubjectRef({
        ID: id,
        Kind: kind,
      }),
    );
  }

  return out;
}

export function pinSubjectKey(subject?: PinSubjectRef | null) {
  const kind = subject?.Kind;
  const id = subject?.ID?.trim();
  return kind && id ? `${kind}:${id}` : "";
}

export function usePinStates(subjects: PinSubjectRef[], profile = PIN_PROFILE) {
  const normalizedSubjects = normalizeSubjects(subjects);
  const signature = JSON.stringify({
    profile,
    subjects: normalizedSubjects.map((subject) => ({
      ID: subject.ID,
      Kind: subject.Kind,
    })),
  });
  const [states, setStates] = useState<Record<string, PinState>>({});

  useEffect(() => {
    const payload = JSON.parse(signature) as {
      profile: string;
      subjects: Array<{ ID: string; Kind: PinSubjectRef["Kind"] }>;
    };
    const requestedSubjects = payload.subjects.map(
      (subject) =>
        new Types.PinSubjectRef({
          ID: subject.ID,
          Kind: subject.Kind,
        }),
    );

    if (requestedSubjects.length === 0) {
      setStates({});
      return;
    }

    let disposed = false;
    let stopListening: (() => void) | undefined;

    const load = async () => {
      const nextStates = await listPinStates(requestedSubjects, payload.profile);
      if (disposed) {
        return;
      }
      const byKey: Record<string, PinState> = {};
      for (const state of nextStates) {
        const key = pinSubjectKey(state.Subject);
        if (key) {
          byKey[key] = state;
        }
      }
      setStates(byKey);
    };

    void load().catch(() => {});
    void subscribePinEvents()
      .then((eventName) => {
        if (disposed) {
          return;
        }
        stopListening = Events.On(eventName, () => {
          void load().catch(() => {});
        });
      })
      .catch(() => {});

    return () => {
      disposed = true;
      stopListening?.();
    };
  }, [signature]);

  return states;
}

export function usePinState(
  subject?: PinSubjectRef | null,
  profile = PIN_PROFILE,
) {
  const states = usePinStates(subject ? [subject] : [], profile);
  const key = pinSubjectKey(subject);
  return key ? states[key] ?? null : null;
}
