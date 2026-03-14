import { createFileRoute } from "@tanstack/react-router";
import { OperationsPage } from "./page";

export const Route = createFileRoute("/operations")({
  component: OperationsPage,
});
