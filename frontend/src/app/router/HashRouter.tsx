import type { PropsWithChildren } from "react";
import { Router } from "wouter";
import { useHashLocation } from "wouter/use-hash-location";

export function HashRouter({ children }: PropsWithChildren) {
  return <Router hook={useHashLocation}>{children}</Router>;
}
