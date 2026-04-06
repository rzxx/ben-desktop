import * as PinFacade from "../../../bindings/ben/desktop/pinfacade";
import {
  Types,
  type PinState,
  type PinSubjectRef,
  type JobSnapshot,
} from "./models";

export const PIN_PROFILE = "desktop";

export function startPin(subject: PinSubjectRef, profile = PIN_PROFILE) {
  return PinFacade.StartPin(
    new Types.PinIntentRequest({
      Profile: profile,
      Subject: subject,
    }),
  ) as Promise<JobSnapshot>;
}

export function unpin(subject: PinSubjectRef, profile = PIN_PROFILE) {
  return PinFacade.Unpin(
    new Types.PinIntentRequest({
      Profile: profile,
      Subject: subject,
    }),
  );
}

export function getPinState(subject: PinSubjectRef, profile = PIN_PROFILE) {
  return PinFacade.GetPinState(
    new Types.PinStateRequest({
      Profile: profile,
      Subject: subject,
    }),
  ) as Promise<PinState>;
}

export function listPinStates(
  subjects: PinSubjectRef[],
  profile = PIN_PROFILE,
): Promise<PinState[]> {
  return PinFacade.ListPinStates(
    new Types.PinStateListRequest({
      Profile: profile,
      Subjects: subjects,
    }),
  );
}

export function subscribePinEvents() {
  return PinFacade.SubscribePinEvents();
}
